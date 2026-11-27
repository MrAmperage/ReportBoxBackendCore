package main

import (
	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/static"
)

func main() { backendcore.Run(app.Info{Name: "static-demo"}, static.Module()) }
