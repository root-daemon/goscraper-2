package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"goscraper/src/globals"
	"goscraper/src/handlers"
	"goscraper/src/helpers/databases"
	"goscraper/src/session"
	"goscraper/src/types"
	"goscraper/src/utils"
	"log"
	"net"
	"os"

	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cache"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

func main() {
	if globals.DevMode {
		godotenv.Load()
	}

	app := fiber.New(fiber.Config{
		Prefork:      false,
		ServerHeader: "GoScraper",
		AppName:      "GoScraper v3.0",
		JSONEncoder:  json.Marshal,
		JSONDecoder:  json.Unmarshal,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return utils.HandleError(c, err)
		},
	})

	app.Use(fiberrecover.New())
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))
	app.Use(etag.New())

	urls := os.Getenv("URL")
	allowedOrigins := "http://localhost:243"
	if urls != "" {
		allowedOrigins += "," + urls
	}

	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,X-CSRF-Token,Authorization",
		ExposeHeaders:    "Content-Length,X-Updated-CSRF-Token",
		AllowCredentials: true,
	}))

	app.Use(limiter.New(limiter.Config{
		Max:        25,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			token := c.Get("X-CSRF-Token")
			if token != "" {
				return utils.Encode(token)
			}
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "🔨 SHUT UP! Rate limit exceeded. Please try again later.",
			})
		},
		SkipFailedRequests: false,
		LimiterMiddleware:  limiter.SlidingWindow{},
	}))

	app.Use(func(c *fiber.Ctx) error {
		switch c.Path() {
		case "/login", "/hello":
			return c.Next()
		}

		token := c.Get("X-CSRF-Token")
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing X-CSRF-Token header",
			})
		}
		return c.Next()
	})

	app.Use(func(c *fiber.Ctx) error {
		switch c.Path() {
		case "/hello":
			return c.Next()
		}

		if globals.DevMode {
			return c.Next()
		}

		token := c.Get("Authorization")
		if token == "" || (!strings.HasPrefix(token, "Bearer ") && !strings.HasPrefix(token, "Token ")) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing Authorization header",
			})
		}

		if strings.HasPrefix(token, "Token ") {
			tokenStr := strings.TrimPrefix(token, "Token ")
			decodedData, err := utils.DecodeBase64(tokenStr)
			if err != nil {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Invalid token: " + tokenStr,
				})
			}

			parts := strings.Split(decodedData, ".")
			if len(parts) < 4 {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Malformed token: " + tokenStr,
				})
			}

			key, _, _, _ := parts[0], parts[1], parts[2], parts[3]

			valid, err := utils.ValidateAuth(fmt.Sprint(time.Now().UnixNano()/int64(time.Millisecond)), key)
			if err != nil || !*valid {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Invalid token: " + tokenStr,
				})
			}
		} else {
			tokenStr := strings.TrimPrefix(token, "Bearer ")
			valid, err := utils.ValidateToken(tokenStr)
			if err != nil || !*valid {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Invalid token: " + tokenStr,
				})
			}
		}

		return c.Next()
	})

	// Universal error handling middleware
	app.Use(func(c *fiber.Ctx) error {
		err := c.Next()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		return nil
	})

	cacheConfig := cache.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Method() != "GET"
		},
		Expiration: 2 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.Path() + "_" + c.Get("X-CSRF-Token")
		},
	}

	api := app.Group("/", func(c *fiber.Ctx) error {
		switch c.Path() {
		case "/login", "/hello":
			return c.Next()
		}
		token := c.Get("X-CSRF-Token")
		if token == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Missing X-CSRF-Token header",
			})
		}
		return c.Next()
	})

	// Routes -----------------------------------------

	app.Get("/hello", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Hello, World!"})
	})

	app.Post("/login", func(c *fiber.Ctx) error {
		var creds struct {
			Username string  `json:"account"`
			Password string  `json:"password"`
			Cdigest  *string `json:"cdigest,omitempty"`
			Captcha  *string `json:"captcha,omitempty"`
		}

		if err := c.BodyParser(&creds); err != nil {
			log.Printf("Error parsing body: %v", err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid JSON body",
			})
		}

		if creds.Username == "" || creds.Password == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Missing account or password",
			})
		}

		lf := &handlers.LoginFetcher{}
		session, err := lf.Login(creds.Username, creds.Password, creds.Cdigest, creds.Captcha)
		if err != nil {
			return err
		}

		if session.Authenticated && session.Cookies != "" {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("login: background goroutine panic: %v", r)
					}
				}()
				user, err := handlers.GetUser(session.Cookies)
				if err != nil || user.RegNumber == "" {
					log.Printf("login: failed to fetch user for credential storage: %v", err)
					return
				}

				db, err := databases.NewDatabaseHelper()
				if err != nil {
					log.Printf("login: failed to init db for credential storage: %v", err)
					return
				}

				encodedToken := utils.Encode(session.Cookies)
				err = db.UpsertData("goscrape", map[string]interface{}{
					"regNumber": user.RegNumber,
					"token":     encodedToken,
					"account":   creds.Username,
					"password":  creds.Password,
					"cookies":   session.Cookies,
				})
				if err != nil {
					log.Printf("login: failed to store credentials: %v", err)
				}
			}()
		}

		return c.JSON(session)
	})

	api.Delete("/logout", func(c *fiber.Ctx) error {
		lf := &handlers.LoginFetcher{}
		session, err := lf.Logout(c.Get("X-CSRF-Token"))
		if err != nil {
			return err
		}
		return c.JSON(session)
	})

	api.Get("/attendance", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		res, err := session.WithAutoRetry(c.Get("X-CSRF-Token"), func(cookie string) (interface{}, error) {
			return handlers.GetAttendance(cookie)
		})
		if err != nil {
			return err
		}
		if res.NewCookies != "" {
			c.Set("X-Updated-CSRF-Token", res.NewCookies)
		}
		return c.JSON(res.Data)
	})

	api.Get("/marks", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		res, err := session.WithAutoRetry(c.Get("X-CSRF-Token"), func(cookie string) (interface{}, error) {
			return handlers.GetMarks(cookie)
		})
		if err != nil {
			return err
		}
		if res.NewCookies != "" {
			c.Set("X-Updated-CSRF-Token", res.NewCookies)
		}
		return c.JSON(res.Data)
	})

	api.Get("/courses", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		res, err := session.WithAutoRetry(c.Get("X-CSRF-Token"), func(cookie string) (interface{}, error) {
			return handlers.GetCourses(cookie)
		})
		if err != nil {
			return err
		}
		if res.NewCookies != "" {
			c.Set("X-Updated-CSRF-Token", res.NewCookies)
		}
		return c.JSON(res.Data)
	})

	api.Get("/user", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		res, err := session.WithAutoRetry(c.Get("X-CSRF-Token"), func(cookie string) (interface{}, error) {
			return handlers.GetUser(cookie)
		})
		if err != nil {
			return err
		}
		if res.NewCookies != "" {
			c.Set("X-Updated-CSRF-Token", res.NewCookies)
		}
		return c.JSON(res.Data)
	})

	api.Get("/calendar", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		calDB, err := databases.NewCalDBHelper()
		if err != nil {
			return err
		}

		dbcal, err := calDB.GetEvents()
		if err != nil {
			return err
		}

		if len(dbcal.Calendar) == 0 {
			res, err := session.WithAutoRetry(c.Get("X-CSRF-Token"), func(cookie string) (interface{}, error) {
				return handlers.GetCalendar(cookie)
			})
			if err != nil {
				return err
			}
			if res.NewCookies != "" {
				c.Set("X-Updated-CSRF-Token", res.NewCookies)
			}
			cal := res.Data.(*types.CalendarResponse)
			go func() {
				for _, event := range cal.Calendar {
					for _, month := range event.Days {
						err = calDB.SetEvent(databases.CalendarEvent{
							ID:        utils.GenerateID(),
							Date:      month.Date,
							Month:     event.Month,
							Day:       month.Day,
							Order:     month.DayOrder,
							Event:     month.Event,
							CreatedAt: time.Now().UnixNano() / int64(time.Millisecond),
						})

						if err != nil {
							log.Printf("Error setting calendar event: %v", err)
							return
						}
					}
				}
			}()
			return c.JSON(cal)
		}

		return c.JSON(dbcal)

	})

	api.Get("/timetable", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		res, err := session.WithAutoRetry(c.Get("X-CSRF-Token"), func(cookie string) (interface{}, error) {
			return handlers.GetTimetable(cookie)
		})
		if err != nil {
			return err
		}
		if res.NewCookies != "" {
			c.Set("X-Updated-CSRF-Token", res.NewCookies)
		}
		return c.JSON(res.Data)
	})

	api.Get("/get", cache.New(cacheConfig), func(c *fiber.Ctx) error {
		token := c.Get("X-CSRF-Token")
		encodedToken := utils.Encode(token)

		db, err := databases.NewDatabaseHelper()
		if err != nil {
			return err
		}

		cachedData, err := db.FindByToken("goscrape", encodedToken)

		if len(cachedData) != 0 &&
			cachedData["timetable"] != nil &&
			cachedData["attendance"] != nil &&
			cachedData["marks"] != nil {

			ophour, err := db.GetOphourByToken(encodedToken)
			if err == nil && ophour != "" {
				cachedData["ophour"] = ophour
			}

			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("/get: background refresh panic: %v", r)
					}
				}()
				data, err := fetchAllData(token)
				if err != nil {
					return
				}
				if data != nil {
					data["token"] = encodedToken
					db.UpsertData("goscrape", data)
				}
			}()

			return c.JSON(cachedData)
		}

		res, err := session.WithAutoRetry(token, func(cookie string) (interface{}, error) {
			return fetchAllData(cookie)
		})
		if err != nil {
			return utils.HandleError(c, err)
		}

		data := res.Data.(map[string]interface{})

		activeToken := token
		if res.NewCookies != "" {
			activeToken = res.NewCookies
			c.Set("X-Updated-CSRF-Token", res.NewCookies)
		}
		data["token"] = utils.Encode(activeToken)

		js, _ := json.Marshal(data)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("/get: background upsert panic: %v", r)
				}
			}()
			db.UpsertData("goscrape", data)
		}()

		var responseData map[string]interface{}
		if err := json.Unmarshal(js, &responseData); err != nil {
			return err
		}
		if res.NewCookies != "" {
			responseData["newCookies"] = res.NewCookies
		}
		return c.JSON(responseData)
	})

	// ----------------------------------------------------

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on port %s...", port)
	ln, err := net.Listen("tcp", "[::]:"+port)
	if err != nil {
		log.Fatalf("Failed to bind: %v", err)
	}
	log.Printf("Starting server on port %s...", port)
	if err := app.Listener(ln); err != nil {
		log.Printf("Server error: %+v", err)
	}
}

func fetchAllData(token string) (map[string]interface{}, error) {
	type result struct {
		key  string
		data interface{}
		err  error
	}

	resultChan := make(chan result, 5)

	go func() {
		data, err := handlers.GetUser(token)
		resultChan <- result{"user", data, err}
	}()
	go func() {
		data, err := handlers.GetAttendance(token)
		resultChan <- result{"attendance", data, err}
	}()
	go func() {
		data, err := handlers.GetMarks(token)
		resultChan <- result{"marks", data, err}
	}()
	go func() {
		data, err := handlers.GetCourses(token)
		resultChan <- result{"courses", data, err}
	}()
	go func() {
		data, err := handlers.GetTimetable(token)
		resultChan <- result{"timetable", data, err}
	}()

	data := make(map[string]interface{})

	for i := 0; i < 5; i++ {
		r := <-resultChan

		if r.err != nil {
			log.Printf(
				"fetchAllData error | key=%s | err=%v",
				r.key,
				r.err,
			)
			log.Printf(
				"partial response so far: %+v",
				data,
			)
			return nil, r.err
		}

		data[r.key] = r.data
	}

	if user, ok := data["user"].(*types.User); ok {
		data["regNumber"] = user.RegNumber
	}

	db, err := databases.NewDatabaseHelper()
	if err != nil {
		log.Printf("database init failed | err=%v", err)
	} else {
		encodedToken := utils.Encode(token)
		ophour, err := db.GetOphourByToken(encodedToken)
		if err != nil {
			log.Printf(
				"ophour fetch failed | token=%s | err=%v",
				encodedToken,
				err,
			)
		} else if ophour != "" {
			data["ophour"] = ophour
		}
	}

	return data, nil
}
