<h1>Reverse tunnel in HTTP using Golang</h1>

<h3>After building, Run client with:</h3><br>
<code>
    ./client example.com:2020 username password
</code><br>

<h3>Server Example:</h3><br>

<code>
func main() {
	server.AddAccounts("username", "password")
	if err := server.SetupAndListen("1000", "1010"); err != nil {
		log.Fatalf("Error for reverse tunnel proxy: ", err)
	}
}
</code>