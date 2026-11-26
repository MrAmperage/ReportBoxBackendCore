package main

import (
	"github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	"github.com/MrAmperage/ReportBoxBackendCore/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"
)

func main() {
	backendcore.Run(app.Info{Name: "telemetry-demo"}, management.Module(), telemetry.Module(), fx.Invoke(func(rt *telemetry.Runtime) error {
		g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "reportbox_demo_value", Help: "Example service metric."})
		g.Set(1)
		return rt.Registerer().Register(g)
	}))
}
