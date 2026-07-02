package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Confirm prints question and reads a y/N answer from stdin.
func Confirm(question string) bool {
	fmt.Print(question)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}
