package auth

type Role string

const (
	Admin    Role = "admin"
	Operator Role = "operator"
	Member   Role = "member"
	Viewer   Role = "viewer"
)

var permissions = map[Role]map[string]bool{
	Admin: {"*": true}, Operator: {"device.view": true, "device.rename": true, "device.disconnect": true, "device.restart": true, "device.shutdown": true, "token.view": true, "audit.view": true}, Member: {"device.view": true}, Viewer: {"device.view": true, "user.view": true, "token.view": true, "audit.view": true},
}

func Allowed(role Role, permission string) bool {
	p := permissions[role]
	return p["*"] || p[permission]
}
