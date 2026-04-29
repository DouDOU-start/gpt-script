package main

func summaryFieldForMode(mode string) string {
	switch mode {
	case "register":
		return "registered"
	case "oauth":
		return "authorized"
	case "login":
		return "authorized"
	default:
		return "planned"
	}
}
