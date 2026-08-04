//go:build !windows && !darwin

package main

import "fmt"

func main() { fmt.Println("TyxNet Tray is currently available on Windows and macOS") }
