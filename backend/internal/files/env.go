package files

import "os"

// envLookup is defined here so files/client.go can read env vars without
// importing config and creating a cycle.
func envLookup(key string) string {
	v, _ := os.LookupEnv(key)
	return v
}
