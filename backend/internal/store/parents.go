package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

const parentSelect = `SELECT p.id, p.school_id, p.user_id, u.name, u.mobile, p.alternate_mobile, p.email,
	p.address, p.status, u.last_login_at, p.created_at, p.updated_at
	FROM parents p JOIN users u ON u.id = p.user_id`

func scanParent(row pgx.Row) (*models.Parent, error) {
	var p models.Parent
	err := row.Scan(&p.ID, &p.SchoolID, &p.UserID, &p.Name, &p.Mobile, &p.AlternateMobile, &p.Email,
		&p.Address, &p.Status, &p.LastLoginAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	p.Children = []models.ChildLink{}
	return &p, nil
}

type ParentProfile struct {
	AlternateMobile string
	Email           string
	Address         string
}

func ListParents(ctx context.Context, q DBTX, schoolID string, f ListFilter, p Page) ([]models.Parent, int, error) {
	// Search also matches the children's names and admission numbers.
	const where = ` WHERE p.school_id = $1 AND ($2 = '' OR p.status = $2)
		AND ($3 = '' OR u.name ILIKE '%' || $3 || '%' OR u.mobile LIKE '%' || $3 || '%'
			OR EXISTS (SELECT 1 FROM parent_students ps JOIN students s ON s.id = ps.student_id
				WHERE ps.parent_id = p.id AND (s.name ILIKE '%' || $3 || '%' OR s.admission_no ILIKE '%' || $3 || '%')))`

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM parents p JOIN users u ON u.id = p.user_id`+where,
		schoolID, f.Status, f.Q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, parentSelect+where+` ORDER BY u.name LIMIT $4 OFFSET $5`,
		schoolID, f.Status, f.Q, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	out := []models.Parent{}
	for rows.Next() {
		pa, err := scanParent(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, *pa)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := attachChildren(ctx, q, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func GetParent(ctx context.Context, q DBTX, schoolID, id string) (*models.Parent, error) {
	p, err := scanParent(q.QueryRow(ctx, parentSelect+` WHERE p.id = $1 AND p.school_id = $2`, id, schoolID))
	if err != nil {
		return nil, err
	}
	list := []models.Parent{*p}
	if err := attachChildren(ctx, q, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// GetParentByUser finds the parent profile behind a parent login.
func GetParentByUser(ctx context.Context, q DBTX, userID string) (*models.Parent, error) {
	return scanParent(q.QueryRow(ctx, parentSelect+` WHERE p.user_id = $1`, userID))
}

func attachChildren(ctx context.Context, q DBTX, parents []models.Parent) error {
	if len(parents) == 0 {
		return nil
	}
	ids := make([]string, len(parents))
	index := map[string]int{}
	for i, p := range parents {
		ids[i] = p.ID
		index[p.ID] = i
	}
	rows, err := q.Query(ctx, `
		SELECT ps.parent_id, s.id, s.name, s.admission_no, s.class, s.section, ps.relationship
		FROM parent_students ps JOIN students s ON s.id = ps.student_id
		WHERE ps.parent_id = ANY($1::uuid[]) ORDER BY s.name`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var parentID string
		var c models.ChildLink
		if err := rows.Scan(&parentID, &c.StudentID, &c.Name, &c.AdmissionNo, &c.Class, &c.Section, &c.Relationship); err != nil {
			return err
		}
		i := index[parentID]
		parents[i].Children = append(parents[i].Children, c)
	}
	return rows.Err()
}

func CreateParent(ctx context.Context, q DBTX, schoolID, userID string, p ParentProfile) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO parents (school_id, user_id, alternate_mobile, email, address)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		schoolID, userID, p.AlternateMobile, p.Email, p.Address).Scan(&id)
	return id, mapErr(err)
}

func UpdateParent(ctx context.Context, q DBTX, schoolID, id string, p ParentProfile) error {
	tag, err := q.Exec(ctx, `UPDATE parents SET alternate_mobile = $3, email = $4, address = $5
		WHERE id = $1 AND school_id = $2`, id, schoolID, p.AlternateMobile, p.Email, p.Address)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func SetParentStatus(ctx context.Context, q DBTX, schoolID, id, status string) error {
	tag, err := q.Exec(ctx, `UPDATE parents SET status = $3 WHERE id = $1 AND school_id = $2`, id, schoolID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type ChildInput struct {
	StudentID    string
	Relationship string
}

// SetParentChildren replaces a parent's linked students. The caller has checked
// that every student belongs to the school.
func SetParentChildren(ctx context.Context, q DBTX, schoolID, parentID string, children []ChildInput) error {
	if _, err := q.Exec(ctx, `DELETE FROM parent_students WHERE parent_id = $1`, parentID); err != nil {
		return err
	}
	for _, c := range children {
		if err := LinkParentStudent(ctx, q, schoolID, parentID, c); err != nil {
			return err
		}
	}
	return nil
}

// LinkParentStudent adds or updates one link.
func LinkParentStudent(ctx context.Context, q DBTX, schoolID, parentID string, c ChildInput) error {
	_, err := q.Exec(ctx, `
		INSERT INTO parent_students (parent_id, student_id, school_id, relationship) VALUES ($1, $2, $3, $4)
		ON CONFLICT (parent_id, student_id) DO UPDATE SET relationship = EXCLUDED.relationship`,
		parentID, c.StudentID, schoolID, c.Relationship)
	return err
}

// CountSchoolStudents returns how many of ids are students of the school (to validate links).
func CountSchoolStudents(ctx context.Context, q DBTX, schoolID string, ids []string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM students WHERE school_id = $1 AND id = ANY($2::uuid[])`, schoolID, ids).Scan(&n)
	return n, err
}

// ParentChild is what the Parent app sees for one linked child.
type ParentChild struct {
	StudentID   string                    `json:"student_id"`
	Name        string                    `json:"name"`
	AdmissionNo string                    `json:"admission_no"`
	Class       string                    `json:"class"`
	Section     string                    `json:"section"`
	Assignment  *models.StudentAssignment `json:"assignment"`
}

// ParentChildren lists the active students linked to a parent login, with their
// current route and stops. This is the only student data a parent may see.
func ParentChildren(ctx context.Context, q DBTX, parentUserID string) ([]ParentChild, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.name, s.admission_no, s.class, s.section, `+assignmentCols+`
		FROM parents p
		JOIN parent_students pst ON pst.parent_id = p.id
		JOIN students s ON s.id = pst.student_id AND s.status = 'active'
		LEFT JOIN student_assignments sa ON sa.student_id = s.id AND sa.ended_at IS NULL`+assignmentJoins+`
		WHERE p.user_id = $1 AND p.status = 'active'
		ORDER BY s.name`, parentUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParentChild{}
	for rows.Next() {
		var c ParentChild
		var a assignmentScan
		if err := rows.Scan(append([]any{&c.StudentID, &c.Name, &c.AdmissionNo, &c.Class, &c.Section}, a.dest()...)...); err != nil {
			return nil, err
		}
		c.Assignment = a.assignment()
		out = append(out, c)
	}
	return out, rows.Err()
}
