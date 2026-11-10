package organization

import "time"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type PageQuery struct {
	Limit  int32
	Offset int64
}
type Organization struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}
type OrganizationView struct {
	Organization Organization
	MyRole       Role
}

type MembershipStatus string

const (
	StatusActive   MembershipStatus = "active"
	StatusDisabled MembershipStatus = "disabled"
)

type Membership struct {
	OrgID     int64
	UserID    int64
	Role      Role
	Status    MembershipStatus
	CreatedAt time.Time
}
type Member struct {
	Membership
	DisplayName string
}
