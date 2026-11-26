package main

import (
	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
)

func main() { backendcore.Run(app.Info{Name: "reportbox-management-demo"}, management.Module()) }
