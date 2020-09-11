package vkern

// Mode ..
type Mode string

// ..
const (
	Performance         = Mode("perf")
	Compatibility       = Mode("compat")
	performanceSuffix   = "P"
	compatibilitySuffix = "C"
)

// KernelSuffix ..
func KernelSuffix(mode Mode) string {

	switch mode {
	case Mode(""):
		fallthrough
	case Compatibility:
		return compatibilitySuffix
	case Performance:
		return performanceSuffix
	default:
		return ""
	}
}

// IsCompatibilityKernel ..
func IsCompatibilityKernel(x Mode) bool {
	switch x {
	case "p":
		fallthrough
	case "performance":
		fallthrough
	case Performance:
		return false
	default:
		return true
	}
}
