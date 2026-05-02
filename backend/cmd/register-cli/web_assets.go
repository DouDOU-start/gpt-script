package main

import "embed"

//go:embed web/dist/*
var webFiles embed.FS
