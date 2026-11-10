package main

func validNick(s string) bool { return nickRe.MatchString(s) }
