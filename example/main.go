package example

import (
	"log"

	"github.com/sinasadeghi83/go-reverse-http-tunnel/server"
)

func main() {
	server.AddAccounts("username", "password")
	if err := server.SetupAndListen("1000", "1010"); err != nil {
		log.Fatalf("Error for reverse tunnel proxy: ", err)
	}
}
