package edge

import "os"

func osReadDir(p string) ([]os.DirEntry, error) { return os.ReadDir(p) }
