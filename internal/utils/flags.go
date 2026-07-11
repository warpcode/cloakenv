package utils

// HasHelpFlag checks if the arguments list contains a help flag (-h or --help) before the command separator "--".
func HasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}
