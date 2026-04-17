# Visual regression tape for `wuphf --help`.
#
# Build the binary first (check.sh does this), then:
#   vhs testdata/vhs/help.tape
#
# Uses the --help flag which has no state or timing and should be
# byte-stable across runs.

Output testdata/vhs/help.gif
Output testdata/vhs/help.txt

Set Shell "bash"
Set FontSize 14
Set Width 1400
Set Height 900
Set TypingSpeed 0ms

Hide
Type "clear"
Enter
Sleep 300ms
Type "/tmp/wuphf-vhs --help"
Enter
Sleep 800ms
Show
Sleep 500ms
