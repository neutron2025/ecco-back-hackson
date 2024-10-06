package controllers

import (
	"blog-auth-server/models"
	"blog-auth-server/utils"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var SecretKey = []byte("SecretKey")

type UserController struct {
	collection  *mongo.Collection
	ctx         context.Context
	redisClient *redis.Client
}

func NewUserController(collection *mongo.Collection, ctx context.Context, redisClient *redis.Client) *UserController {
	return &UserController{
		collection:  collection,
		ctx:         ctx,
		redisClient: redisClient,
	}
}

type Signup struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type AddPermission struct {
	Username   string             `json:"username"`
	Permission models.Permissions `json:"permission"`
}
type LoginResp struct {
	hash string
}

// 添加新的结构体用于更新用户的 PowAddress
type UpdatePowAddress struct {
	PowAddress string `json:"powaddr"`
}

// 设置用户的 PowAddress
func (uc *UserController) SetPowAddress(c *fiber.Ctx) error {
	// 从 JWT 获取用户 ID
	claims := c.Locals("claims").(jwt.MapClaims)
	userID, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "未授权"})
	}

	// 解析请求体
	var updateInfo UpdatePowAddress
	if err := c.BodyParser(&updateInfo); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的请求数据"})
	}

	// 验证 PowAddress
	if updateInfo.PowAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "权证地址不能为空"})
	}

	// 更新用户信息
	objectID, _ := primitive.ObjectIDFromHex(userID)
	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": bson.M{"powaddr": updateInfo.PowAddress}}

	_, err := uc.collection.UpdateOne(uc.ctx, filter, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "更新用户信息失败"})
	}

	return c.JSON(fiber.Map{"message": "权证地址更新成功"})
}

// 添加新的结构体用于更新用户的 Pow
type UpdatePow struct {
	UserID string  `json:"user_id"`
	Pow    float64 `json:"pow"`
}

// 设置用户的 Pow
func (uc *UserController) SetPow(c *fiber.Ctx) error {
	// 解析请求体
	var updateInfo UpdatePow
	if err := c.BodyParser(&updateInfo); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的请求数据"})
	}

	// 验证 UserID
	if updateInfo.UserID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "用户ID不能为空"})
	}

	// 验证 Pow
	if updateInfo.Pow < 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "POW值不能为负数"})
	}

	// 更新用户信息
	objectID, err := primitive.ObjectIDFromHex(updateInfo.UserID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的用户ID"})
	}

	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": bson.M{"pow": updateInfo.Pow}}

	result, err := uc.collection.UpdateOne(uc.ctx, filter, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "更新用户POW失败"})
	}

	if result.MatchedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定用户"})
	}

	return c.JSON(fiber.Map{"message": "POW更新成功"})
}

func (uc *UserController) CreateUser(c *fiber.Ctx) error {
	signupReq := new(Signup)
	user := new(models.User)

	if err := c.BodyParser(signupReq); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Bad Request")
	}
	hashedPassword, err := utils.HashPassword(signupReq.Password)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Server Error")
	}

	// 检查用户名是否已经存在
	filter := bson.M{}
	if utils.IsValidEmail(signupReq.Username) {
		filter["email"] = signupReq.Username
	} else if utils.IsValidPhone(signupReq.Username) {
		filter["phone"] = signupReq.Username
	} else {
		return c.JSON(fiber.Map{"message": "please input Email or phoneNumber"})
	}

	// 查找是否已存在该用户
	var existingUser models.User
	err = uc.collection.FindOne(uc.ctx, filter).Decode(&existingUser)
	if err == nil {
		// 如果用户已存在，返回错误
		return c.JSON(fiber.Map{"message": "Email or phone number already exists"})
	}
	// 用户不存在，继续注册流程
	user.ID = primitive.NewObjectID()
	user.CreatedAt = time.Now()
	user.Password = hashedPassword
	user.Permissions = models.Permissions{AdminFlag: false}
	// user.Pow = 0.0       // 显式设置 pow 的默认值
	// user.PowAddress = "" // 显式设置 powAddress 的默认值

	if utils.IsValidEmail(signupReq.Username) {
		user.Email = signupReq.Username
	} else if utils.IsValidPhone(signupReq.Username) {
		user.Phone = signupReq.Username
	}

	savedUser, err := uc.collection.InsertOne(uc.ctx, user)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Unable to save user")
	}
	log.Println("User Created", savedUser)
	return c.JSON(fiber.Map{"message": "Success"})
}

func (uc *UserController) CreateAdminUser(c *fiber.Ctx) error {

	// 假设这是用户输入的密码
	AdminPassword := "adminpassword"
	AdminUsername := "adminuser"
	signupAdmin := new(Signup)
	signupAdmin.Username = AdminUsername
	signupAdmin.Password = AdminPassword

	// 生成密码的散列值
	hashedPassword, err := utils.HashPassword(signupAdmin.Password)
	if err != nil {
		// 处理错误
		return fiber.NewError(fiber.StatusInternalServerError, "Faild to generate password hash")
	}

	// 检查是否已经存在管理员
	user := new(models.User)
	err = uc.collection.FindOne(uc.ctx, bson.M{"phone": AdminUsername, "permissions.admin_flag": true}).Decode(user)
	if err == nil {
		// 如果没有错误，说明找到了一个管理员
		return c.JSON(fiber.Map{"message": "Administrator already exists"})
	} else if err != mongo.ErrNoDocuments {
		// 如果错误不是 "没有文档"，处理其他可能的错误
		return fiber.NewError(fiber.StatusInternalServerError, "Error checking for existing administrator")
	}

	// 如果没有找到管理员，创建一个新的管理员
	user = &models.User{
		Phone:       AdminUsername,
		ID:          primitive.NewObjectID(),
		CreatedAt:   time.Now(),
		Password:    hashedPassword,
		Permissions: models.Permissions{AdminFlag: true},
	}

	// user.Phone = signupAdmin.Username
	// user.ID = primitive.NewObjectID()
	// user.CreatedAt = time.Now()
	// user.Password = hashedPassword
	// user.Permissions = models.Permissions{AdminFlag: true}

	savedUser, err := uc.collection.InsertOne(uc.ctx, user)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Unable to save Administrator")
	}
	log.Println("Administrator Created", savedUser)
	return c.JSON(fiber.Map{"message": "Success"})

}

func (uc *UserController) Login(c *fiber.Ctx) error {
	signupReq := new(Signup)
	if err := c.BodyParser(signupReq); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Bad Request")
	}
	user := new(models.User)

	if utils.IsValidEmail(signupReq.Username) {
		err := uc.collection.FindOne(uc.ctx, bson.D{{"email", signupReq.Username}}).Decode(&user)
		if err != nil {
			return c.JSON(fiber.Map{"message": "Invalid username or password"})
		}
		err = utils.VerifyPassword(signupReq.Password, user.Password)
		if err != nil {
			return c.JSON(fiber.Map{"message": "Invalid username or password"})
		}
	} else if utils.IsValidPhone(signupReq.Username) {
		err := uc.collection.FindOne(uc.ctx, bson.D{{"phone", signupReq.Username}}).Decode(&user)
		if err != nil {
			return c.JSON(fiber.Map{"message": "Invalid username or password"})
		}
		err = utils.VerifyPassword(signupReq.Password, user.Password)
		if err != nil {
			return c.JSON(fiber.Map{"message": "Invalid username or password"})
		}
	} else {
		return c.JSON(fiber.Map{"message": "please input Email or phoneNumber"})
	}

	objStr := fmt.Sprintf("%+v", user.Permissions) //序列化
	data := []byte(objStr)
	hasher := sha256.New()
	_, err := hasher.Write(data)
	if err != nil {
		log.Fatal("Error:", err)
		return err
	}
	hash := hasher.Sum(nil)
	hashString := hex.EncodeToString(hash)
	token := jwt.New(jwt.SigningMethodHS256)             //创建JWT
	claims := token.Claims.(jwt.MapClaims)               //设置JWT声明
	claims["hash"] = hashString                          //设置JWT声明
	claims["exp"] = time.Now().Add(time.Hour * 1).Unix() //设置JWT声明
	claims["user_id"] = user.ID                          // 确保这里的键是 "user_id"

	permissionsJSON, err := json.Marshal(user.Permissions)                                              //使用json.Marshal方法将user.Permissions序列化为JSON格式的字节切片
	result, err := uc.redisClient.SetNX(uc.ctx, hashString, permissionsJSON, 3600*time.Second).Result() //使用redisClient.SetNX方法将序列化后的权限数据存储到Redis中,0表示设置的键没有过期时间  3600s
	log.Println("ERR", err)
	log.Println("Result from redis", result)
	tokenString, err := token.SignedString(SecretKey) //生成JWT令牌
	if err != nil {
		log.Fatal("Error signing token:", err)
		return err
	}
	log.Println("JWT Token:", tokenString)
	return c.JSON(fiber.Map{"token": tokenString}) //使用fiber.Map构建一个包含JWT令牌的JSON响应,返回给客户端
}

func (uc *UserController) TestRoute(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"quality": "Admin-Test-Route-SecretKey"})
}

func (uc *UserController) AllUsers(c *fiber.Ctx) error {
	page, err := strconv.Atoi(c.Query("page", "1"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid page parameter"})
	}
	limit, err := strconv.Atoi(c.Query("limit", "20"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid limit parameter"})
	}
	skip := (page - 1) * limit
	findOptions := options.Find()
	findOptions.SetSkip(int64(skip))
	findOptions.SetLimit(int64(limit))

	cursor, err := uc.collection.Find(uc.ctx, bson.D{}, findOptions)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}
	defer cursor.Close(uc.ctx)

	var users []models.User
	for cursor.Next(uc.ctx) {
		var user models.User
		if err := cursor.Decode(&user); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error decoding user"})
		}
		users = append(users, user)
	}

	if err := cursor.Err(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Cursor Error: " + err.Error()})
	}

	totalCount, err := uc.collection.CountDocuments(uc.ctx, bson.D{})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error counting users"})
	}

	response := struct {
		Users []models.User `json:"users"`
		Total int64         `json:"total"`
	}{
		Users: users,
		Total: totalCount,
	}

	return c.Status(fiber.StatusOK).JSON(response)

}

func (uc *UserController) GetOneUser(c *fiber.Ctx) error {

	// 从路径参数中获取用户ID
	userID := c.Params("id")

	// 验证用户ID格式
	if len(userID) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "User ID is required"})
	}

	// 尝试将字符串用户ID转换为ObjectID
	objectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid User ID"})
	}

	// 创建用于查询的用户模型
	var user models.User
	// 执行查询，获取单个用户
	err = uc.collection.FindOne(uc.ctx, bson.M{"_id": objectID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// 用户不存在
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		// 查询出错
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	// 发送用户信息作为响应
	return c.JSON(user)
}

func (uc *UserController) GetUserInfo(c *fiber.Ctx) error {
	// 从JWT令牌中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		log.Println("Error: claims not found in context locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		log.Println("Error: user_id claim not found or not a string")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	// 将用户ID转换为ObjectID
	objectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		log.Printf("Error converting user ID to ObjectID: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid User ID"})
	}

	// 创建用于查询的用户模型
	var user models.User
	// 执行查询，获取当前用户的信息
	err = uc.collection.FindOne(uc.ctx, bson.M{"_id": objectID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}
	// 发送当前用户信息作为响应
	return c.JSON(user)
}

func (uc *UserController) DelUser(c *fiber.Ctx) error {
	userID := c.Params("id")

	if len(userID) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "UserID is required"})
	}

	objectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid User ID"})
	}

	filter := bson.M{"_id": objectID}

	deleteRestlt, err := uc.collection.DeleteOne(uc.ctx, filter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	if deleteRestlt.DeletedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	// 返回成功的响应
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "User deleted successfully"})
}
