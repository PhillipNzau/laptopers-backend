package routes

import (
	"github.com/gin-gonic/gin"
	config "github.com/phillip/contribution-tracker-go/config"
	controllers "github.com/phillip/contribution-tracker-go/controllers"
	middleware "github.com/phillip/contribution-tracker-go/middleware"
)

func SetupRoutes(r *gin.Engine, cfg *config.Config) {
	// public
	r.POST("/auth/register", controllers.Register(cfg))
	r.POST("/auth/login", controllers.Login(cfg))
	r.POST("/auth/refresh", controllers.RefreshToken(cfg))

	// otp
	r.POST("/auth/request-otp", controllers.RequestOTP(cfg))
	r.POST("/auth/verify-otp", controllers.VerifyOTP(cfg))

	// protected
	auth := middleware.AuthMiddleware(cfg)

	users := r.Group("/users")
	users.Use(auth)
	{
		// users.POST("", controllers.ListUsers(cfg))
		users.GET("", controllers.ListUsers(cfg))
		users.GET(":id", controllers.GetUser(cfg))
		users.PATCH(":id", controllers.UpdateUser(cfg))
		users.DELETE(":id", controllers.DeleteUser(cfg))
	}

	notifs := r.Group("/notifications")
	notifs.Use(auth) // protected
	{
		notifs.GET("", controllers.ListNotifications(cfg))
		notifs.PATCH("/:id/read", controllers.MarkNotificationRead(cfg))
	}

	// Events
	hubs := r.Group("/hubs")
	hubs.Use(auth)
	{
		hubs.POST("", controllers.CreateHub(cfg))
		hubs.GET("", controllers.ListHubs(cfg))
		hubs.GET("/:id", controllers.GetHub(cfg))
		hubs.PATCH("/:id", controllers.UpdateHub(cfg))
		hubs.DELETE("/:id", controllers.DeleteHub(cfg))
	}



}
