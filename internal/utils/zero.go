package utils

// ZeroBytes overwrites every byte in the slice with zero to clear sensitive data from memory.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
