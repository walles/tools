package invertifstatement

import "os"

func F1() int {
	if len(os.Args) > 2 { // want "invert if condition"
		return 1
	} else {
		return 2
	}
}
