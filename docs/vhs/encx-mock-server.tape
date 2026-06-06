# encx-mock: local server startup and encli over HTTP
# Pause guide: short ~3.5s, medium ~5s, large ~7s

Output ../gifs/encx-mock-server.gif

Source config.tape

Require encli
Require encx-mock

Hide
Type "export PATH=$PWD/bin:$PATH ENCX_MOCK_ADDR=127.0.0.1:18080"
Enter
Type "encx-mock >/tmp/encx-mock-vhs.log 2>&1 &"
Enter
Sleep 2s
Type "encli logout -domain 127.0.0.1:18080 2>/dev/null; encli login -domain 127.0.0.1:18080 -http -login demo -password demo"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# encx-mock — локальный mock Encounter для разработки"
Enter
Sleep 1.5s

Type "cat /tmp/encx-mock-vhs.log"
Enter
Sleep 5s

Type "encli game-list -domain 127.0.0.1:18080 -http"
Enter
Sleep 3.5s

Type "encli status -domain 127.0.0.1:18080 -http -game-id 424242"
Enter
Sleep 7s

Type "encli send-code -domain 127.0.0.1:18080 -http -game-id 424242 1"
Enter
Sleep 3.5s

Hide
Type "kill %1 2>/dev/null || true"
Enter
