//go:build !windows

package browsersignup

import "syscall"

func hideWindowAttr() *syscall.SysProcAttr {
	return nil
}
