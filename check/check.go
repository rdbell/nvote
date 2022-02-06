package check

// TODO: more checkerr functions for alerts & logging

// Panic panics if err
func Panic(err error) {
	if err != nil {
		panic(err)
	}
}
