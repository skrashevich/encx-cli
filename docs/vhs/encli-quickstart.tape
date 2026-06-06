# encli quickstart: login → game-list → status → level → send-code
# Uses public mock at encounter.exe.xyz (HTTPS).
# Pause guide: short ~3s, medium ~4.5s, large ~6s

Output ../gifs/encli-quickstart.gif

Source config.tape

Require encli

Hide
Type "export PATH=$PWD/bin:$PATH ENCX_DOMAIN=encounter.exe.xyz"
Enter
Type "encli logout -domain encounter.exe.xyz 2>/dev/null; encli login -domain encounter.exe.xyz -login vhs-quickstart -password demo"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# encli — логин, список игр, статус и отправка кода"
Enter
Sleep 1.5s

Type "encli game-list"
Enter
Sleep 3.5s

Type "encli status -game-id 424242"
Enter
Sleep 6s

Type "encli level -game-id 424242"
Enter
Sleep 4s

Type "encli send-code -game-id 424242 REVIEW-START"
Enter
Sleep 3.5s
