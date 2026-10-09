package store

import (
	"context"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

// AssignStudent ends the student's current assignment (if any) and starts a new one.
// The caller has already checked that the route and stops belong to the school.
func AssignStudent(ctx context.Context, q DBTX, schoolID, studentID, routeID string, pickupStopID, dropStopID *string, by string) error {
	if err := EndAssignment(ctx, q, schoolID, studentID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO student_assignments (school_id, student_id, route_id, pickup_stop_id, drop_stop_id, assigned_by)
		VALUES ($1, $2, $3, $4, $5, $6)`, schoolID, studentID, routeID, pickupStopID, dropStopID, by)
	return mapErr(err)
}

// EndAssignment closes the current assignment, keeping it as history. No-op if none.
func EndAssignment(ctx context.Context, q DBTX, schoolID, studentID string) error {
	_, err := q.Exec(ctx, `UPDATE student_assignments SET ended_at = now()
		WHERE student_id = $1 AND school_id = $2 AND ended_at IS NULL`, studentID, schoolID)
	return err
}

// AssignmentHistory lists all assignments of a student, newest first.
func AssignmentHistory(ctx context.Context, q DBTX, schoolID, studentID string) ([]models.StudentAssignment, error) {
	rows, err := q.Query(ctx, `SELECT `+assignmentCols+` FROM student_assignments sa`+assignmentJoins+`
		WHERE sa.student_id = $1 AND sa.school_id = $2 ORDER BY sa.assigned_at DESC`, studentID, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.StudentAssignment{}
	for rows.Next() {
		var a assignmentScan
		if err := rows.Scan(a.dest()...); err != nil {
			return nil, err
		}
		out = append(out, *a.assignment())
	}
	return out, rows.Err()
}

// RouteStopIDs returns the IDs of the route's stops (for validating an assignment).
func RouteStopIDs(ctx context.Context, q DBTX, routeID string) (map[string]bool, error) {
	rows, err := q.Query(ctx, `SELECT id FROM stops WHERE route_id = $1`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// CountStopAssignments counts students currently assigned to a stop (pickup or drop).
func CountStopAssignments(ctx context.Context, q DBTX, stopID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM student_assignments
		WHERE ended_at IS NULL AND (pickup_stop_id = $1 OR drop_stop_id = $1)`, stopID).Scan(&n)
	return n, err
}

// RouteStudent is the transport-only view of a student on a route (route page, S3-10).
type RouteStudent struct {
	StudentID    string  `json:"student_id"`
	Name         string  `json:"name"`
	AdmissionNo  string  `json:"admission_no"`
	Class        string  `json:"class"`
	Section      string  `json:"section"`
	PickupStopID *string `json:"pickup_stop_id"`
	DropStopID   *string `json:"drop_stop_id"`
}

func RouteStudents(ctx context.Context, q DBTX, schoolID, routeID string) ([]RouteStudent, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.name, s.admission_no, s.class, s.section, sa.pickup_stop_id, sa.drop_stop_id
		FROM student_assignments sa JOIN students s ON s.id = sa.student_id
		WHERE sa.route_id = $1 AND sa.school_id = $2 AND sa.ended_at IS NULL AND s.status = 'active'
		ORDER BY s.name`, routeID, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RouteStudent{}
	for rows.Next() {
		var rs RouteStudent
		if err := rows.Scan(&rs.StudentID, &rs.Name, &rs.AdmissionNo, &rs.Class, &rs.Section, &rs.PickupStopID, &rs.DropStopID); err != nil {
			return nil, err
		}
		out = append(out, rs)
	}
	return out, rows.Err()
}
