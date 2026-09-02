package utils

// ZeroBytes overwrites every byte in the slice with zero to clear sensitive
// data from memory.
//
// ZeroBytes is marked noinline so the compiler cannot eliminate the final stores
// as dead writes after inlining — without this barrier, a future Go release
// could silently turn zeroization into a no-op, defeating the security guarantee.
//
//go:noinline
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
