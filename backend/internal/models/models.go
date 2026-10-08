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
