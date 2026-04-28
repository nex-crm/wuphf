package embedding

import "os"

// getEnvForTest is the test-only indirection for os.Getenv. Lets the
// openai_test.go skip predicate read like prose without importing os in
// every test file.
func getEnvForTest(k string) string { return os.Getenv(k) }
