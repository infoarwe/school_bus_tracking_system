// Package models holds the domain types shared by the store and handlers.
package models

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleSuperAdmin       Role = "super_admin"
	RoleSchoolAdmin      Role = "school_admin"
	RoleTransportManager Role = "transport_manager"
	RoleDriver           Role = "driver"
	RoleParent           Role = "parent"
)

// WebRoles log in to the admin web with email + password.
var WebRoles = []Role{RoleSuperAdmin, RoleSchoolAdmin, RoleTransportManager}

// AppRoles log in to the mobile apps with mobile + OTP.
var AppRoles = []Role{RoleDriver, RoleParent}

func (r Role) IsWeb() bool {
	return r == RoleSuperAdmin || r == RoleSchoolAdmin || r == RoleTransportManager
}
func (r Role) IsApp() bool { return r == RoleDriver || r == RoleParent }

const (
	StatusActive      = "active"
	StatusInactive    = "inactive"
	StatusSuspended   = "suspended"
	StatusMaintenance = "maintenance"
)

type School struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Code            string          `json:"code"`
	Address         string          `json:"address"`
	City            string          `json:"city"`
	State           string          `json:"state"`
	Pincode         string          `json:"pincode"`
	ContactName     string          `json:"contact_name"`
	ContactPhone    string          `json:"contact_phone"`
	ContactEmail    string          `json:"contact_email"`
	WorkingDays     []string        `json:"working_days"`
	Timezone        string          `json:"timezone"`
	TransportConfig json.RawMessage `json:"transport_config"`
	Status          string          `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type User struct {
	ID           string     `json:"id"`
	SchoolID     *string    `json:"school_id"`
	Role         Role       `json:"role"`
	Name         string     `json:"name"`
	Email        *string    `json:"email"`
	Mobile       *string    `json:"mobile"`
	Status       string     `json:"status"`
	TOTPEnabled  bool       `json:"totp_enabled"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	PasswordHash *string    `json:"-"`
	TOTPSecret   *string    `json:"-"`
}

type Session struct {
	ID         string    `json:"id"`
	DeviceName string    `json:"device_name"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

// Driver joins the drivers row with its login user (name, mobile, last login).
type Driver struct {
	ID                    string     `json:"id"`
	SchoolID              string     `json:"school_id"`
	UserID                string     `json:"user_id"`
	Name                  string     `json:"name"`
	Mobile                string     `json:"mobile"`
	LicenseNumber         string     `json:"license_number"`
	LicenseExpiry         *string    `json:"license_expiry"` // YYYY-MM-DD
	Address               string     `json:"address"`
	EmergencyContactName  string     `json:"emergency_contact_name"`
	EmergencyContactPhone string     `json:"emergency_contact_phone"`
	IDProofType           string     `json:"id_proof_type"`
	IDProofNumber         string     `json:"id_proof_number"`
	Notes                 string     `json:"notes"`
	Status                string     `json:"status"`
	LastLoginAt           *time.Time `json:"last_login_at"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type Bus struct {
	ID            string    `json:"id"`
	SchoolID      string    `json:"school_id"`
	VehicleNumber string    `json:"vehicle_number"`
	Capacity      int       `json:"capacity"`
	MakeModel     string    `json:"make_model"`
	GPSDeviceID   string    `json:"gps_device_id"`
	Notes         string    `json:"notes"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Route struct {
	ID             string    `json:"id"`
	SchoolID       string    `json:"school_id"`
	Name           string    `json:"name"`
	Code           string    `json:"code"`
	StartPoint     string    `json:"start_point"`
	Description    string    `json:"description"`
	SupportsPickup bool      `json:"supports_pickup"`
	SupportsDrop   bool      `json:"supports_drop"`
	Status         string    `json:"status"`
	StopCount      int       `json:"stop_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Stops          []Stop    `json:"stops,omitempty"` // only on GET /routes/{id}
}

type Stop struct {
	ID              string    `json:"id"`
	RouteID         string    `json:"route_id"`
	Name            string    `json:"name"`
	Landmark        string    `json:"landmark"`
	Latitude        float64   `json:"latitude"`
	Longitude       float64   `json:"longitude"`
	Sequence        int       `json:"sequence"`
	PickupTime      *string   `json:"pickup_time"` // HH:MM
	DropTime        *string   `json:"drop_time"`   // HH:MM
	GeofenceRadiusM int       `json:"geofence_radius_m"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	TransportUses    = "uses_transport"
	TransportNotUsed = "not_using"
)

type Student struct {
	ID              string  `json:"id"`
	SchoolID        string  `json:"school_id"`
	AdmissionNo     string  `json:"admission_no"`
	Name            string  `json:"name"`
	Class           string  `json:"class"`
	Section         string  `json:"section"`
	Notes           *string `json:"notes,omitempty"` // nil for Transport Managers (transport-only view)
	Status          string  `json:"status"`
	TransportStatus string  `json:"transport_status"`
	// Current route/stop assignment, or null if not assigned.
	Assignment *StudentAssignment `json:"assignment"`
	// Linked parents; only on GET of a single student.
	Parents   []ParentLink `json:"parents,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// StopRef is the part of a stop shown alongside an assignment.
type StopRef struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Sequence   int     `json:"sequence"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	PickupTime *string `json:"pickup_time"`
	DropTime   *string `json:"drop_time"`
}

type StudentAssignment struct {
	ID             string     `json:"id"`
	RouteID        string     `json:"route_id"`
	RouteCode      string     `json:"route_code"`
	RouteName      string     `json:"route_name"`
	PickupStop     *StopRef   `json:"pickup_stop"`
	DropStop       *StopRef   `json:"drop_stop"`
	AssignedByName *string    `json:"assigned_by_name"`
	AssignedAt     time.Time  `json:"assigned_at"`
	EndedAt        *time.Time `json:"ended_at"`
}

type ParentLink struct {
	ParentID     string `json:"parent_id"`
	Name         string `json:"name"`
	Mobile       string `json:"mobile"`
	Relationship string `json:"relationship"`
}

type ChildLink struct {
	StudentID    string `json:"student_id"`
	Name         string `json:"name"`
	AdmissionNo  string `json:"admission_no"`
	Class        string `json:"class"`
	Section      string `json:"section"`
	Relationship string `json:"relationship"`
}

// Parent joins the parents row with its login user (name, mobile, last login).
type Parent struct {
	ID              string      `json:"id"`
	SchoolID        string      `json:"school_id"`
	UserID          string      `json:"user_id"`
	Name            string      `json:"name"`
	Mobile          string      `json:"mobile"`
	AlternateMobile string      `json:"alternate_mobile"`
	Email           string      `json:"email"`
	Address         string      `json:"address"`
	Status          string      `json:"status"`
	LastLoginAt     *time.Time  `json:"last_login_at"`
	Children        []ChildLink `json:"children"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

const (
	TripMorningPickup = "morning_pickup"
	TripEveningDrop   = "evening_drop"

	TripScheduled = "scheduled"
	TripConfirmed = "confirmed"
	TripStarted   = "started"
	TripCompleted = "completed"
	TripCancelled = "cancelled"
)

// Trip is one daily assignment: Driver + Bus + Route + Date + Trip Type.
type Trip struct {
	ID       string `json:"id"`
	SchoolID string `json:"school_id"`
	TripDate string `json:"trip_date"` // YYYY-MM-DD in the school's time zone
	TripType string `json:"trip_type"` // morning_pickup | evening_drop
	Status   string `json:"status"`
	Route    struct {
		ID        string `json:"id"`
		Code      string `json:"code"`
		Name      string `json:"name"`
		StopCount int    `json:"stop_count"`
	} `json:"route"`
	Bus struct {
		ID            string `json:"id"`
		VehicleNumber string `json:"vehicle_number"`
		Capacity      int    `json:"capacity"`
	} `json:"bus"`
	Driver struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Mobile string `json:"mobile"`
	} `json:"driver"`
	// Students assigned to the route with a stop for this trip type.
	StudentCount int        `json:"student_count"`
	Notes        string     `json:"notes"`
	CancelReason string     `json:"cancel_reason"`
	ConfirmedAt  *time.Time `json:"confirmed_at"`
	StartedAt    *time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type TripStatusChange struct {
	FromStatus    *string   `json:"from_status"`
	ToStatus      string    `json:"to_status"`
	ChangedByName *string   `json:"changed_by_name"`
	ChangedByRole string    `json:"changed_by_role"`
	Reason        string    `json:"reason"`
	CreatedAt     time.Time `json:"created_at"`
}
