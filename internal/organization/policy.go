package organization

// Membership is fetched from the database for this request, never from a token.
func membershipPermission(m Membership, write bool) error {
	if m.Status != StatusActive {
		return ErrNotFound
	}
	if m.Role != RoleAdmin && m.Role != RoleMember {
		return ErrStorage
	}
	if write && m.Role != RoleAdmin {
		return ErrForbidden
	}
	return nil
}
