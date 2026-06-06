# encx-mock: scenario mode banner and PZDC network-drop simulation
# Pause guide: short ~3s, medium ~4.5s, large ~6s, hang demo ~5s

Output ../gifs/encx-mock-features.gif

Source config.tape

Require encli
Require encx-mock

Hide
Type "export PATH=$PWD/bin:$PATH ENCX_MOCK_ADDR=127.0.0.1:18081"
Enter
Type "encx-mock -scenario ../../cmd/encx-mock/fixtures/app_store_review_scenario.html >/tmp/encx-mock-vhs2.log 2>&1 &"
Enter
Sleep 2s
Type "encli logout -domain 127.0.0.1:18081 2>/dev/null; encli login -domain 127.0.0.1:18081 -http -login pzdc -password demo"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# encx-mock — сценарий из HTML и эмуляция потери сети (PZDC)"
Enter
Sleep 1.5s

Type "encx-mock -v"
Enter
Sleep 2.5s

Type "head -5 /tmp/encx-mock-vhs2.log"
Enter
Sleep 5s

Type "encli status -domain 127.0.0.1:18081 -http -game-id 424242"
Enter
Sleep 6s

Type "encli send-code -domain 127.0.0.1:18081 -http -game-id 424242 PZDC"
Enter
Sleep 3s

Type "encli send-code -domain 127.0.0.1:18081 -http -game-id 424242 REVIEW-START"
Enter
Sleep 5s

Type "# через ~1 мин сеть восстанавливается; можно сменить login:password"
Enter
Sleep 4s

Hide
Type "kill %1 2>/dev/null || true"
Enter
