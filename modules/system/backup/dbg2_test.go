package backup

import (
	"fmt"
	"io"
)

func fmt2(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "SQL> "+format+"\n", args...)
}
