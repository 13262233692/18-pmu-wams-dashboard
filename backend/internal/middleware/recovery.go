package middleware

import (
	"log"
	"runtime/debug"
	"time"

	"github.com/gofiber/fiber/v2"
)

func PanicRecovery() fiber.Handler {
	return func(c *fiber.Ctx) error {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Printf("PANIC RECOVERED in HTTP handler: %v\n%s", r, stack)

				c.Status(500).JSON(fiber.Map{
					"error":   "Internal server error",
					"message": "An unexpected error occurred",
					"time":    time.Now().Format(time.RFC3339),
				})
			}
		}()
		return c.Next()
	}
}

func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Printf("PANIC RECOVERED in goroutine '%s': %v\n%s", name, r, stack)
			}
		}()
		fn()
	}()
}

func SafeGoWithRecovery(name string, fn func(), onPanic func(interface{})) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Printf("PANIC RECOVERED in goroutine '%s': %v\n%s", name, r, stack)
				if onPanic != nil {
					func() {
						defer func() {
							if pr := recover(); pr != nil {
								log.Printf("PANIC RECOVERED in panic handler for '%s': %v", name, pr)
							}
						}()
						onPanic(r)
					}()
				}
			}
		}()
		fn()
	}()
}

type PanicProtectedProcessor struct {
	Name          string
	PanicCount    uint64
	LastPanicTime time.Time
	LastPanicInfo string
}

func (pp *PanicProtectedProcessor) Process(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			pp.PanicCount++
			pp.LastPanicTime = time.Now()
			pp.LastPanicInfo = string(stack)
			log.Printf("PANIC RECOVERED in processor '%s' (total=%d): %v\n%s",
				pp.Name, pp.PanicCount, r, stack)
		}
	}()
	fn()
}
