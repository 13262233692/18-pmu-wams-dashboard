package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"wams-dashboard/internal/buffer"
	"wams-dashboard/internal/control"
	"wams-dashboard/internal/detector"
	"wams-dashboard/internal/filter"
	"wams-dashboard/internal/middleware"
	"wams-dashboard/internal/models"
	"wams-dashboard/internal/network"
	"wams-dashboard/internal/protocol"
	"wams-dashboard/internal/simulator"
	"wams-dashboard/internal/watchdog"
	wshub "wams-dashboard/internal/websocket"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

var (
	oscSystem      *detector.LowFreqOscillationSystem
	ctrlExecutor   *control.ControlExecutor
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC RECOVERED in main: %v", r)
			log.Println("Attempting graceful shutdown after main panic...")
			shutdown()
		}
	}()

	app := fiber.New(fiber.Config{
		Prefork:       false,
		CaseSensitive: true,
		StrictRouting: true,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
	}))
	app.Use(logger.New())
	app.Use(middleware.PanicRecovery())

	phasorBuffer := buffer.NewLockFreeRingBuffer(50000)
	swrlsFilter := filter.NewSWRLSFilter(50, 0.98)
	wsHub := wshub.NewHub()
	middleware.SafeGo("ws-hub", wsHub.Run)

	alertChan := make(chan *models.OscillationAlert, 100)
	controlChan := make(chan *models.ControlAction, 100)

	ctrlExecutor = control.NewControlExecutor(
		alertChan,
		controlChan,
		wsHub.BroadcastControl,
	)
	ctrlExecutor.Start()

	oscSystem = detector.NewLowFreqOscillationSystem(
		wsHub.GetSections(),
		alertChan,
		controlChan,
	)
	middleware.SafeGo("osc-detector", oscSystem.RunAnalysisLoop)

	app.Get("/ws", fiberws.New(func(c *fiberws.Conn) {
		wsHub.HandleConnection(c)
	}))

	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"service": "WAMS Dashboard Backend",
		})
	})

	app.Get("/api/phasors/latest", func(c *fiber.Ctx) error {
		data := phasorBuffer.GetAll()
		return c.JSON(fiber.Map{
			"count": len(data),
			"data":  data,
		})
	})

	app.Get("/api/watchdog/stats", func(c *fiber.Ctx) error {
		stats := watchdog.GetWatchdogManager().GetAllStats()
		overallLevel := watchdog.GetWatchdogManager().GetOverallAlertLevel()
		return c.JSON(fiber.Map{
			"alertLevel": overallLevel,
			"stats":      stats,
		})
	})

	app.Get("/api/oscillation/status", func(c *fiber.Ctx) error {
		statuses := oscSystem.GetAllStatus()
		snapshots := oscSystem.GetSnapshots()
		return c.JSON(fiber.Map{
			"statuses":  statuses,
			"snapshots": snapshots,
		})
	})

	app.Get("/api/control/status", func(c *fiber.Ctx) error {
		ctrlStatus := ctrlExecutor.GetStatus()
		actions := ctrlExecutor.GetExecutedActions()
		return c.JSON(fiber.Map{
			"executor": ctrlStatus,
			"actions":  actions,
			"count":    len(actions),
		})
	})

	parsedChan := make(chan *models.PhasorMeasurement, 50000)
	filteredChan := make(chan *models.PhasorMeasurement, 50000)

	filterProc := &middleware.PanicProtectedProcessor{Name: "swrls-filter"}
	middleware.SafeGo("filter-pipeline", func() {
		for pm := range parsedChan {
			filterProc.Process(func() {
				filtered := swrlsFilter.Apply(pm)
				filteredChan <- filtered
			})
		}
	})

	bufferProc := &middleware.PanicProtectedProcessor{Name: "buffer-broadcast"}
	middleware.SafeGo("buffer-pipeline", func() {
		for pm := range filteredChan {
			bufferProc.Process(func() {
				phasorBuffer.Push(pm)
				wsHub.Broadcast(pm)
				oscSystem.IngestPMU(pm)
			})
		}
	})

	udpAddr := ":4712"
	udpListener := network.NewZeroCopyUDPListener(udpAddr)
	udpParser := protocol.NewIEEEParser()
	middleware.SafeGo("udp-listener", func() {
		udpListener.Listen(func(data []byte) {
			measurements, err := udpParser.Parse(data)
			if err == nil {
				for _, m := range measurements {
					parsedChan <- m
				}
			}
		})
	})

	tcpAddr := ":4712"
	tcpListener := network.NewZeroCopyTCPListener(tcpAddr)
	tcpParser := protocol.NewIEEEParser()
	middleware.SafeGo("tcp-listener", func() {
		tcpListener.Listen(func(data []byte) {
			measurements, err := tcpParser.Parse(data)
			if err == nil {
				for _, m := range measurements {
					parsedChan <- m
				}
			}
		})
	})

	pmuSim := simulator.NewPMUSimulator(8)
	middleware.SafeGo("pmu-simulator", func() {
		pmuSim.Start(parsedChan)
	})

	log.Println("WAMS Dashboard Backend starting...")
	log.Printf("  HTTP API  -> http://localhost:8080")
	log.Printf("  WebSocket -> ws://localhost:8080/ws")
	log.Printf("  PMU UDP   -> %s", udpAddr)
	log.Printf("  PMU TCP   -> %s", tcpAddr)
	log.Printf("  Osc Detector: 6 sections, Prony order=10, window=400 samples")
	log.Printf("  Control Executor: max 10 actions/hr, cooldown 30s")
	log.Printf("  Simulated Oscillations: 3 negative-damping modes scheduled")

	middleware.SafeGo("http-server", func() {
		if err := app.Listen(":8080"); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	})

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdown(app, phasorBuffer, udpListener, tcpListener, pmuSim, oscSystem, ctrlExecutor, parsedChan, filteredChan, alertChan, controlChan)
}

func shutdown(components ...interface{}) {
	fmt.Println("\nShutting down gracefully...")
	for _, comp := range components {
		switch c := comp.(type) {
		case *fiber.App:
			c.Shutdown()
		case *buffer.LockFreeRingBuffer:
			c.Close()
		case *network.ZeroCopyUDPListener:
			c.Close()
		case *network.ZeroCopyTCPListener:
			c.Close()
		case *simulator.PMUSimulator:
			c.Stop()
		case *detector.LowFreqOscillationSystem:
			c.Stop()
		case *control.ControlExecutor:
			c.Stop()
		case chan *models.PhasorMeasurement:
			close(c)
		case chan *models.OscillationAlert:
			close(c)
		case chan *models.ControlAction:
			close(c)
		}
	}
	watchdog.GetWatchdogManager().StopAll()
	fmt.Println("Shutdown complete.")
}
