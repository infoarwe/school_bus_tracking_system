package notify

import (
	"errors"
	"regexp"

	"github.com/google/uuid"
)

// ErrInvalidTopic: an ID used to build a topic name was not a valid UUID, or
// the resulting topic name was not one FCM accepts. Topic construction never
// takes IDs from client input — only from already-validated database records
// (school_id, route_id, stop_id) — so this should only ever fire on a
// programming error, not on attacker input.
var ErrInvalidTopic = errors.New("invalid topic identifier")

// fcmTopicPattern mirrors Firebase's own constraint on topic names:
// https://firebase.google.com/docs/cloud-messaging/manage-topics
var fcmTopicPattern = regexp.MustCompile(`^[a-zA-Z0-9-_.~%]{1,900}$`)

func validTopic(topic string) (string, error) {
	if !fcmTopicPattern.MatchString(topic) {
		return "", ErrInvalidTopic
	}
	return topic, nil
}

func validID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrInvalidTopic
	}
	return nil
}

// SchoolTopic: whole-school announcements.
func SchoolTopic(schoolID string) (string, error) {
	if err := validID(schoolID); err != nil {
		return "", err
	}
	return validTopic("school_" + schoolID)
}

// RouteTopic: bus started/completed/cancelled, school reached, delay,
// breakdown, route announcements — everything tripRiders/RouteRiders serve
// without filtering to one stop.
func RouteTopic(schoolID, routeID string) (string, error) {
	if err := validID(schoolID); err != nil {
		return "", err
	}
	if err := validID(routeID); err != nil {
		return "", err
	}
	return validTopic("school_" + schoolID + "_route_" + routeID)
}

// StopTopic: approaching/reached/crossed for one specific stop only. Parents
// subscribe to this for each of their child's pickup and drop stops (which may
// be the same stop row for both directions).
func StopTopic(schoolID, routeID, stopID string) (string, error) {
	if err := validID(schoolID); err != nil {
		return "", err
	}
	if err := validID(routeID); err != nil {
		return "", err
	}
	if err := validID(stopID); err != nil {
		return "", err
	}
	return validTopic("school_" + schoolID + "_route_" + routeID + "_stop_" + stopID)
}
