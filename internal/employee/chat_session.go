package employee

// EmployeeChatSessionID is the standing chat session for one employee
// (routing-funnel C4). The id is stable: "emp-sess-" plus the employee id.
// It is not a random bot-exec conversation id.
func EmployeeChatSessionID(employeeID string) string {
	return "emp-sess-" + employeeID
}
