# encli gameplay: levels, sectors, bonuses, hints, codes, log
# Uses public mock at encounter.exe.xyz (HTTPS).
# Pause guide: short ~3.5s, medium ~4.5s, large ~6s

Output ../gifs/encli-gameplay.gif

Source config.tape

Require encli

Hide
Type "export PATH=$PWD/bin:$PATH ENCX_DOMAIN=encounter.exe.xyz"
Enter
Type "encli logout -domain encounter.exe.xyz 2>/dev/null; encli login -domain encounter.exe.xyz -login vhs-gameplay -password demo"
Enter
Type "encli send-code -game-id 424242 REVIEW-START >/dev/null 2>&1"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# encli — уровни, секторы, бонусы, подсказки и лог пробитий"
Enter
Sleep 1.5s

Type "encli levels -game-id 424242"
Enter
Sleep 4s

Type "encli sectors -game-id 424242"
Enter
Sleep 4s

Type "encli bonuses -game-id 424242"
Enter
Sleep 4.5s

Type "encli hints -game-id 424242"
Enter
Sleep 4s

Type "encli send-code -game-id 424242 MAP-101"
Enter
Sleep 3s

Type "encli send-code -game-id 424242 BONUS-5"
Enter
Sleep 3s

Type "encli log -game-id 424242"
Enter
Sleep 6s
