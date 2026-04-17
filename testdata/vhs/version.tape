# Visual regression tape for `wuphf --version`.
#
# Build the binary first (check.sh does this), then:
#   vhs testdata/vhs/version.tape
#
# The --version flag prints a single deterministic line.

Output testdata/vhs/version.gif
Output testdata/vhs/version.txt

Set Shell "bash"
Set FontSize 14
Set Width 800
Set Height 200
Set TypingSpeed 0ms

Hide
Type "clear"
Enter
Sleep 300ms
Type "/tmp/wuphf-vhs --version"
Enter
Sleep 500ms
Show
Sleep 500ms
