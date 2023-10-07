package main

import (
	"goplaygroundapp/routes"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	routes.Register(app)
	app.Listen(":3000")
	routes.HandleCleanup()
}
