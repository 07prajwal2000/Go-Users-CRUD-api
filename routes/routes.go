package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"goplaygroundapp/types"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

var DB *sqlx.DB
var redisCache *redis.Client
var cacheContext = context.Background()

func init() {
	redisCache = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	db, err := sqlx.Connect("postgres", "user=postgres dbname=goplayground password=12345 sslmode=disable")
	if err != nil {
		fmt.Println("Error connecting db. " + err.Error())
		return
	}
	DB = db
}

func Register(app *fiber.App) {
	app.Get("/users", getAllUsers)
	app.Get("/users/:id", getUser)
	app.Post("/users", createUser)
	app.Put("/users/:id", updateUser)
	app.Delete("/users/:id", deleteUser)
}

func HandleCleanup() {
	DB.Close()
	redisCache.Close()
}

func getUser(c *fiber.Ctx) error {
	id, _ := c.ParamsInt("id", -1)
	if id == -1 {
		return c.Status(http.StatusBadRequest).SendString("invalid id in the param")
	}
	key := fmt.Sprintf("user_%d", id)
	userJson, err := redisCache.Get(cacheContext, key).Result()
	foundCache := true
	if err != nil {
		foundCache = false
	}
	if foundCache {
		cacheUser := &types.User{}
		fromJson(userJson, cacheUser)
		return c.JSON(cacheUser)
	}
	user, err := getUserFromDb(id)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("server error while finding user")
	}
	if user == nil {
		return c.Status(http.StatusNotFound).SendString("user not found")
	}
	redisCache.Set(cacheContext, key, toJson(*user), 0)
	return c.JSON(user)
}

func deleteUser(c *fiber.Ctx) error {
	id, _ := c.ParamsInt("id", -1)
	if id == -1 {
		return c.Status(http.StatusBadRequest).SendString("invalid id in the param")
	}
	sql := `DELETE FROM users
	WHERE id = $1;`
	rows, err := DB.Exec(sql, id)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("server error")
	}
	total, err := rows.RowsAffected()
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("server error")
	}
	if total == 0 {
		return c.Status(http.StatusBadRequest).SendString(fmt.Sprintf("user with id: %d not found", id))
	}
	redisCache.Del(cacheContext, fmt.Sprintf("user_%d", id))
	return c.Status(http.StatusOK).SendString("deleted")
}

func updateUser(c *fiber.Ctx) error {
	userDto := &types.CreateUserDto{}
	id, _ := c.ParamsInt("id", -1)

	if id == -1 {
		return c.Status(http.StatusBadRequest).SendString("invalid id in the param")
	}
	if err := c.BodyParser(userDto); err != nil {
		return c.Status(http.StatusBadRequest).
			SendString("invalid data")
	}
	if _, err := DB.Exec(`
		UPDATE users 
		set firstname =$1, lastname = $2, age = $3 
		WHERE id = $4`,
		&userDto.FirstName, &userDto.LastName, &userDto.Age, &id); err != nil {
		return c.Status(http.StatusInternalServerError).
			SendString("error saving to database")
	}
	user := &types.User{
		Id:        id,
		FirstName: userDto.FirstName,
		LastName:  userDto.LastName,
		Age:       userDto.Age,
	}
	redisCache.Set(cacheContext, fmt.Sprintf("user_%d", id), toJson(user), 0)
	c.Response().Header.Set("Location", fmt.Sprintf("/users/%d", id))
	return c.Status(http.StatusCreated).JSON(user)
}

func createUser(c *fiber.Ctx) error {
	userDto := &types.CreateUserDto{}
	if err := c.BodyParser(userDto); err != nil {
		return c.Status(http.StatusBadRequest).
			SendString("invalid data")
	}

	if _, err := DB.Exec("insert into users(firstname, lastname, age) values($1, $2, $3)", &userDto.FirstName, &userDto.LastName, &userDto.Age); err != nil {
		return c.Status(http.StatusInternalServerError).
			SendString("error saving to database")
	}
	id, err := getLastIdOfUsers()
	if err != nil {
		return c.Status(http.StatusInternalServerError).
			SendString("error getting the latest id")
	}
	user := &types.User{
		Id:        id,
		FirstName: userDto.FirstName,
		LastName:  userDto.LastName,
		Age:       userDto.Age,
	}
	redisCache.Set(cacheContext, fmt.Sprintf("user_%d", id), toJson(user), 0)
	c.Response().Header.Set("Location", fmt.Sprintf("/users/%d", id))
	return c.Status(http.StatusCreated).JSON(user)
}

func getAllUsers(c *fiber.Ctx) error {
	users := make([]types.User, 0)
	scanner := redisCache.Scan(cacheContext, 0, "prefix:user_", 0).Iterator()
	cacheFound := true
	if scanner.Err() != nil {
		cacheFound = false
		return c.Status(http.StatusInternalServerError).SendString("server error while loading cache")
	}
	for cacheFound && scanner.Next(cacheContext) {
		key := scanner.Val()
		value, err := redisCache.Get(cacheContext, key).Result()
		if err != nil {
			continue
		}
		newUser := &types.User{}
		fromJson(value, newUser)
		users = append(users, *newUser)
	}
	rows, err := DB.Query("select * from users")
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("server error while getting data")
	}
	for rows.Next() {
		newUser := &types.User{}
		if rows.Scan(&newUser.Id, &newUser.FirstName, &newUser.LastName, &newUser.Age) != nil {
			continue
		}
		users = append(users, *newUser)
	}
	return c.Status(200).JSON(users)
}

func getLastIdOfUsers() (int, error) {
	row := DB.QueryRow("select max(id) from users")
	id := 0
	err := row.Scan(&id)
	return id, err
}

func getUserFromDb(id int) (*types.User, error) {
	row, err := DB.Query("select * from users where id=$1 limit 1", id)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	newUser := &types.User{}
	if row.Next() {
		row.Scan(&newUser.Id, &newUser.FirstName, &newUser.LastName, &newUser.Age)
		return newUser, nil
	}
	return nil, nil
}

func toJson(v any) string {
	val, _ := json.Marshal(v)
	return string(val)
}

func fromJson(v string, obj any) {
	json.Unmarshal([]byte(v), obj)
}
