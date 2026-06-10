package main

import (
	"fmt"

	"github.com/zcag/tela/backend/internal/auth"
)

// throwaway: mint a PAT (raw, prefix, hmac) using the same scheme the server uses,
// reading TELA_API_KEY_SECRET from env. Prints TSV. Deleted after benchmarking.
func main() {
	raw, prefix, hmacHex, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\t%s\t%s\n", raw, prefix, hmacHex)
}
