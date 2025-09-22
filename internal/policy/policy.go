package policy

type Policy = string

const (
	PolicyOperator       Policy = "operator"
	PolicyContainer      Policy = "container"
	PolicyMCP            Policy = "mcp"
	PolicyScratchNonRoot Policy = "scratch-nonroot"
	PolicyScratchRoot    Policy = "scratch-root"
	PolicyRoot           Policy = "root"
	PolicyKonflux        Policy = "konflux"
)
