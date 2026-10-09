package notify

import (
	"context"
	"sort"
	"strings"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// Notify stores a notification for the given recipients and queues one push per
// device. With a DedupeKey it fires at most once: a repeat returns created=false.
//
// Recipients may list the same parent once per child; the parent then gets one
// push whose title names the children ("Asha, Arjun · Bus approaching") and one
// inbox row per child.
func Notify(ctx context.Context, q store.DBTX, n store.NotificationInput, riders []store.Rider) (id string, created bool, err error) {
	if len(riders) == 0 {
		return "", false, nil // nobody to tell; do not burn the dedupe key either
	}
	if n.Data == nil {
		n.Data = map[string]string{}
	}
	n.Data["type"] = n.Type
	if id, created, err = store.InsertNotification(ctx, q, n); err != nil || !created {
		return id, created, err
	}
	n.Data["notification_id"] = id
	if err := store.InsertRecipients(ctx, q, id, riders); err != nil {
		return "", false, err
	}

	// Group by parent: child names for the title, child IDs for the app to open.
	type person struct{ names, studentIDs []string }
	people := map[string]*person{}
	var order []string
	for _, r := range riders {
		p := people[r.UserID]
		if p == nil {
			p = &person{}
			people[r.UserID] = p
			order = append(order, r.UserID)
		}
		if r.StudentID != nil {
			p.names = append(p.names, r.StudentName)
			p.studentIDs = append(p.studentIDs, *r.StudentID)
		}
	}
	tokens, err := store.ActiveDeviceTokens(ctx, q, order)
	if err != nil {
		return "", false, err
	}
	var deliveries []store.DeliveryInput
	for _, userID := range order {
		p := people[userID]
		title := n.Title
		data := cloneMap(n.Data)
		if len(p.names) > 0 {
			sort.Strings(p.names)
			title = strings.Join(p.names, ", ") + " · " + n.Title
			data["student_ids"] = strings.Join(p.studentIDs, ",")
		}
		for _, tok := range tokens[userID] {
			deliveries = append(deliveries, store.DeliveryInput{UserID: userID, Token: tok, Title: title, Body: n.Body, Data: data})
		}
	}
	if err := store.InsertDeliveries(ctx, q, id, deliveries); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
