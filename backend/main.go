package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"wams-dashboard/internal/buffer"
	"wams-dashboard/internal/filter"
	"wams-dashboard/internal/models"
	"wams-dashboard/internal/network"
	"wams-dashboard/internal/protocol"
	"wams-dashboard/internal/simulator"
	wshub "wams-dashboard/internal/websocket"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
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

	phasorBuffer := buffer.NewLockFreeRingBuffer(10000)
	swrlsFilter := filter.NewSWRLSFilter(50, 0.98)
	wsHub := wshub.NewHub()
	go wsHub.Run()

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

	parsedChan := make(chan *models.PhasorMeasurement, 10000)
	filteredChan := make(chan *models.PhasorMeasurement, 10000)

	go func() {
		for pm := range parsedChan {
			filtered := swrlsFilter.Apply(pm)
			filteredChan <- filtered
		}
	}()

	go func() {
		for pm := range filteredChan {
			phasorBuffer.Push(pm)
			wsHub.Broadcast(pm)
		}
	}()

	udpAddr := ":4712"
	udpListener := network.NewZeroCopyUDPListener(udpAddr)
	udpParser := protocol.NewIEEEParser()
	go udpListener.Listen(func(data []byte) {
		measurements, err := udpParser.Parse(data)
		if err == nil {
			for _, m := range measurements {
				parsedChan <- m
			}
		}
	})

	tcpAddr := ":4712"
	tcpListener := network.NewZeroCopyTCPListener(tcpAddr)
	tcpParser := protocol.NewIEEEParser()
	go tcpListener.Listen(func(data []byte) {
		measurements, err := tcpParser.Parse(data)
		if err == nil {
			for _, m := range measurements {
				parsedChan <- m
			}
		}
	})

	pmuSim := simulator.NewPMUSimulator(8)
	go pmuSim.Start(parsedChan)

	log.Println("WAMS Dashboard Backend starting...")
	log.Printf("  HTTP API  -> http://localhost:8080")
	log.Printf("  WebSocket -> ws://localhost:8080/ws")
	log.Printf("  PMU UDP   -> %s", udpAddr)
	log.Printf("  PMU TCP   -> %s", tcpAddr)

	go func() {
		if err := app.Listen(":8080"); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down gracefully...")
	app.Shutdown()
	udpListener.Close()
	tcpListener.Close()
	pmuSim.Stop()
	close(parsedChan)
	close(filteredChan)
}
