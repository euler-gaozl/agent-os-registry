package registry

// ModelManagementProfile is also emitted by the standalone binary before
// configuration/database initialization for managed release compatibility.
func ModelManagementProfile() map[string]any {
	return map[string]any{"format": 1, "provides": "agent-model-native-v1", "final_output": "receipt-field-v1"}
}
