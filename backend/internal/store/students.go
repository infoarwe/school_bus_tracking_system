package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// Assignment columns + joins, shared by the student list and assignment history.
// The caller supplies the student_assignments alias "sa".
const assignmentCols = `sa.id, sa.route_id, r.code, r.name, sa.assigned_at, sa.ended_at, ab.name,
	ps.id, ps.name, ps.sequence, ps.latitude, ps.longitude, to_char(ps.pickup_time, 'HH24:MI'), to_char(ps.drop_time, 'HH24:MI'),
	ds.id, ds.name, ds.sequence, ds.latitude, ds.longitude, to_char(ds.pickup_time, 'HH24:MI'), to_char(ds.drop_time, 'HH24:MI')`

const assignmentJoins = `
	LEFT JOIN routes r ON r.id = sa.route_id
	LEFT JOIN stops ps ON ps.id = sa.pickup_stop_id
	LEFT JOIN stops ds ON ds.id = sa.drop_stop_id
	LEFT JOIN users ab ON ab.id = sa.assigned_by`

// nullable holders for one LEFT JOINed assignment.
type assignmentScan struct {
	id, routeID, routeCode, routeName, assignedBy *string
	assignedAt, endedAt                           *time.Time
	pickup, drop                                  stopRefScan
}

type stopRefScan struct {
	id, name, pickupTime, dropTime *string
	seq                            *int
	lat, lng                       *float64
}

func (a *assignmentScan) dest() []any {
	return []any{&a.id, &a.routeID, &a.routeCode, &a.routeName, &a.assignedAt, &a.endedAt, &a.assignedBy,
		&a.pickup.id, &a.pickup.name, &a.pickup.seq, &a.pickup.lat, &a.pickup.lng, &a.pickup.pickupTime, &a.pickup.dropTime,
		&a.drop.id, &a.drop.name, &a.drop.seq, &a.drop.lat, &a.drop.lng, &a.drop.pickupTime, &a.drop.dropTime}
}

func (s *stopRefScan) ref() *models.StopRef {
	if s.id == nil {
		return nil
	}
	return &models.StopRef{ID: *s.id, Name: *s.name, Sequence: *s.seq, Latitude: *s.lat, Longitude: *s.lng,
		PickupTime: s.pickupTime, DropTime: s.dropTime}
}

func (a *assignmentScan) assignment() *models.StudentAssignment {
	if a.id == nil {
		return nil
	}
	return &models.StudentAssignment{
		ID: *a.id, RouteID: *a.routeID, RouteCode: *a.routeCode, RouteName: *a.routeName,
		AssignedAt: *a.assignedAt, EndedAt: a.endedAt, AssignedByName: a.assignedBy,
		PickupStop: a.pickup.ref(), DropStop: a.drop.ref(),
	}
}

const studentSelect = `SELECT s.id, s.school_id, s.admission_no, s.name, s.class, s.section, s.notes, s.status,
	s.transport_status, s.created_at, s.updated_at, ` + assignmentCols + `
	FROM students s
	LEFT JOIN student_assignments sa ON sa.student_id = s.id AND sa.ended_at IS NULL` + assignmentJoins

func scanStudent(row pgx.Row) (*models.Student, error) {
	var st models.Student
	var notes string
	var a assignmentScan
	dest := append([]any{&st.ID, &st.SchoolID, &st.AdmissionNo, &st.Name, &st.Class, &st.Section, &notes,
		&st.Status, &st.TransportStatus, &st.CreatedAt, &st.UpdatedAt}, a.dest()...)
	if err := row.Scan(dest...); err != nil {
		return nil, mapErr(err)
	}
	st.Notes = &notes
	st.Assignment = a.assignment()
	return &st, nil
}

type StudentFilter struct {
	Q               string
	Status          string
	TransportStatus string
	Class           string
	Section         string
	RouteID         string
	Assigned        string // "yes", "no" or ""
}

func ListStudents(ctx context.Context, q DBTX, schoolID string, f StudentFilter, p Page) ([]models.Student, int, error) {
	const where = ` WHERE s.school_id = $1
		AND ($2 = '' OR s.status = $2)
		AND ($3 = '' OR s.transport_status = $3)
		AND ($4 = '' OR s.class = $4)
		AND ($5 = '' OR s.section = $5)
		AND ($6 = '' OR sa.route_id::text = $6)
		AND ($7 = '' OR ($7 = 'yes') = (sa.id IS NOT NULL))
		AND ($8 = '' OR s.name ILIKE '%' || $8 || '%' OR s.admission_no ILIKE '%' || $8 || '%')`
	args := []any{schoolID, f.Status, f.TransportStatus, f.Class, f.Section, f.RouteID, f.Assigned, f.Q}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM students s
		LEFT JOIN student_assignments sa ON sa.student_id = s.id AND sa.ended_at IS NULL`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, studentSelect+where+` ORDER BY s.class, s.section, s.name LIMIT $9 OFFSET $10`,
		append(args, p.PageSize, p.Offset())...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Student{}
	for rows.Next() {
		st, err := scanStudent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *st)
	}
	return out, total, rows.Err()
}

func GetStudent(ctx context.Context, q DBTX, schoolID, id string) (*models.Student, error) {
	return scanStudent(q.QueryRow(ctx, studentSelect+` WHERE s.id = $1 AND s.school_id = $2`, id, schoolID))
}

// GetStudentByAdmissionNo is used by the CSV import to update existing students.
func GetStudentByAdmissionNo(ctx context.Context, q DBTX, schoolID, admissionNo string) (*models.Student, error) {
	return scanStudent(q.QueryRow(ctx, studentSelect+` WHERE s.admission_no = $1 AND s.school_id = $2`, admissionNo, schoolID))
}

type StudentInput struct {
	AdmissionNo     string
	Name            string
	Class           string
	Section         string
	Notes           string
	TransportStatus string
}

func CreateStudent(ctx context.Context, q DBTX, schoolID string, in StudentInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO students (school_id, admission_no, name, class, section, notes, transport_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		schoolID, in.AdmissionNo, in.Name, in.Class, in.Section, in.Notes, in.TransportStatus).Scan(&id)
	return id, mapErr(err)
}

func UpdateStudent(ctx context.Context, q DBTX, schoolID, id string, in StudentInput) error {
	tag, err := q.Exec(ctx, `
		UPDATE students SET admission_no = $3, name = $4, class = $5, section = $6, notes = $7, transport_status = $8
		WHERE id = $1 AND school_id = $2`,
		id, schoolID, in.AdmissionNo, in.Name, in.Class, in.Section, in.Notes, in.TransportStatus)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func SetStudentStatus(ctx context.Context, q DBTX, schoolID, id, status string) error {
	tag, err := q.Exec(ctx, `UPDATE students SET status = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StudentParents lists the parents linked to a student.
func StudentParents(ctx context.Context, q DBTX, studentID string) ([]models.ParentLink, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, u.name, u.mobile, ps.relationship
		FROM parent_students ps
		JOIN parents p ON p.id = ps.parent_id
		JOIN users u ON u.id = p.user_id
		WHERE ps.student_id = $1
		ORDER BY u.name`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ParentLink{}
	for rows.Next() {
		var l models.ParentLink
		if err := rows.Scan(&l.ParentID, &l.Name, &l.Mobile, &l.Relationship); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// StudentClasses returns the distinct class/section values, for filter dropdowns.
func StudentClasses(ctx context.Context, q DBTX, schoolID string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT class FROM students WHERE school_id = $1 AND class <> '' ORDER BY class`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
