package check

// Panic panics if err
func Panic(err error) {
	if err != nil {
		panic(err)
	}
}
