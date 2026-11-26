package telemetry

import (
	"github.com/MrAmperage/ReportBoxBackendCore/nats"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"github.com/prometheus/client_golang/prometheus"
)

// Scrape snapshots only: no database queries or broker calls.
func registerSources(reg *prometheus.Registry, broker *nats.Connection, db *postgres.Pool) error {
	if broker != nil {
		connected := prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "reportbox_nats_connected", Help: "Whether the shared NATS connection is connected."}, func() float64 {
			n, e := broker.Conn()
			if e == nil && n.IsConnected() {
				return 1
			}
			return 0
		})
		if e := reg.Register(connected); e != nil {
			return e
		}
		for _, v := range []struct {
			name  string
			value func() float64
		}{
			{"reportbox_nats_messages_sent_total", func() float64 {
				n, e := broker.Conn()
				if e != nil {
					return 0
				}
				return float64(n.Stats().OutMsgs)
			}},
			{"reportbox_nats_messages_received_total", func() float64 {
				n, e := broker.Conn()
				if e != nil {
					return 0
				}
				return float64(n.Stats().InMsgs)
			}},
			{"reportbox_nats_reconnects_total", func() float64 {
				n, e := broker.Conn()
				if e != nil {
					return 0
				}
				return float64(n.Stats().Reconnects)
			}},
		} {
			if e := reg.Register(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: v.name, Help: "Native NATS connection statistics."}, v.value)); e != nil {
				return e
			}
		}
	}
	if db != nil {
		for _, v := range []struct {
			name  string
			value func() float64
		}{
			{"reportbox_postgres_connections_acquired", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().AcquiredConns())
			}},
			{"reportbox_postgres_connections_idle", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().IdleConns())
			}},
			{"reportbox_postgres_connections_total", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().TotalConns())
			}},
			{"reportbox_postgres_connections_max", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().MaxConns())
			}},
		} {
			if e := reg.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: v.name, Help: "Native pgxpool snapshot."}, v.value)); e != nil {
				return e
			}
		}
		if e := reg.Register(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "reportbox_postgres_acquire_seconds_total", Help: "Cumulative successful acquisition duration."}, func() float64 {
			p, e := db.Native()
			if e != nil {
				return 0
			}
			return p.Stat().AcquireDuration().Seconds()
		})); e != nil {
			return e
		}
		for _, v := range []struct {
			name  string
			value func() float64
		}{
			{"reportbox_postgres_acquires_total", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().AcquireCount())
			}},
			{"reportbox_postgres_empty_acquires_total", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().EmptyAcquireCount())
			}},
			{"reportbox_postgres_canceled_acquires_total", func() float64 {
				p, e := db.Native()
				if e != nil {
					return 0
				}
				return float64(p.Stat().CanceledAcquireCount())
			}},
		} {
			if e := reg.Register(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: v.name, Help: "Native pgxpool acquisition statistics."}, v.value)); e != nil {
				return e
			}
		}

	}
	return nil
}
