package controllers

import (
	"blog-auth-server/models"
	"context"
	"log"

	"github.com/dgrijalva/jwt-go"
	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type CartController struct {
	cartCollection    *mongo.Collection // 用于操作购物车的集合
	productCollection *mongo.Collection // 用于操作产品的集合
	ctx               context.Context
}

// NewCartController 构造函数
func NewCartController(cartCollection, productCollection *mongo.Collection, ctx context.Context) *CartController {
	return &CartController{
		cartCollection:    cartCollection,
		productCollection: productCollection,
		ctx:               ctx,
	}
}

func (cc *CartController) AddtoCart(c *fiber.Ctx) error {
	// 解析前端传递的JSON数据
	var addToCartReq struct {
		Products []struct {
			ProductID string `json:"product_id"`
			Quantity  int    `json:"quantity"`
			Size      string `json:"size"`
			Color     string `json:"color"`
		} `json:"products"`
	}
	if err := c.BodyParser(&addToCartReq); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request data"})
	}
	// 从中间件获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		log.Println("Error: claims not found in context locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		log.Println("Error: user_id claim not found or not a string")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	// 将user_id字符串转换为primitive.ObjectID
	var userID primitive.ObjectID

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"}) // 如果转换失败，处理错误
	}

	// 检查用户购物车是否存在，如果不存在则创建新的购物车
	var cart models.Cart
	findCart := bson.M{"user_ref": userID}
	cartExists := true
	err = cc.cartCollection.FindOne(cc.ctx, findCart).Decode(&cart)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			cart = models.Cart{
				ID:        primitive.NewObjectID(),
				UserRef:   userID,
				CartItems: []models.CartItem{},
			}
			cartExists = false
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error checking for existing cart"})
		}
	}

	// 遍历产品列表，添加到购物车
	for _, item := range addToCartReq.Products {
		// 将字符串格式的产品ID转换为primitive.ObjectID
		productID, err := primitive.ObjectIDFromHex(item.ProductID)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid product ID"})
		}

		// 检查产品是否存在
		var product models.Product

		findProduct := bson.M{"_id": productID}
		err = cc.productCollection.FindOne(cc.ctx, findProduct).Decode(&product)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "One or more products not found"})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error checking product existence"})
		}

		// 检查购物车中是否已经有该产品
		found := false
		for i, cartItem := range cart.CartItems {
			if cartItem.ProductRef == productID && cartItem.Size == item.Size && cartItem.Color == item.Color {
				cart.CartItems[i].Quantity += item.Quantity // 增加数量
				found = true
				break
			}
		}
		if !found {
			// 如果购物车中没有该产品，添加新的购物车项
			cart.CartItems = append(cart.CartItems, models.CartItem{
				ProductRef: productID,
				Quantity:   item.Quantity,
				Size:       item.Size,
				Color:      item.Color,
			})
		}
	}

	// 保存购物车到数据库
	if !cartExists {
		// 如果购物车是新创建的，插入数据库
		_, err = cc.cartCollection.InsertOne(cc.ctx, cart)
	} else {
		// 如果购物车已存在，更新数据库记录
		updateResult, err := cc.cartCollection.UpdateOne(cc.ctx, findCart, bson.M{"$set": bson.M{"items": cart.CartItems}})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error updating cart"})
		}
		if updateResult.MatchedCount == 0 {
			_, err = cc.cartCollection.InsertOne(cc.ctx, cart)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error creating new cart"})
			}
		}
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error saving cart"})
	}

	// 返回成功响应
	return c.JSON(fiber.Map{"message": "Product added to cart successfully"})
}

func (cc *CartController) DelfromCart(c *fiber.Ctx) error {
	// 从请求体中获取要删除的商品信息
	var deleteItem struct {
		ProductRef string `json:"ProductRef"`
		Size       string `json:"Size"`
		Color      string `json:"Color"`
	}
	if err := c.BodyParser(&deleteItem); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的请求数据"})
	}

	// 这里假设你已经在中间件中设置了用户ID
	// 从JWT令牌中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		log.Println("Error: claims not found in context locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}
	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		log.Println("Error: user_id claim not found or not a string")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	productID, err := primitive.ObjectIDFromHex(deleteItem.ProductRef)
	// 将user_id字符串转换为primitive.ObjectID
	var userID primitive.ObjectID

	userID, err = primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		// 如果转换失败，处理错误
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}
	// 更新购物车，删除指定商品
	filter := bson.M{"user_ref": userID}
	update := bson.M{
		"$pull": bson.M{
			"items": bson.M{
				"product_ref": productID,
				"size":        deleteItem.Size,
				"color":       deleteItem.Color,
			},
		},
	}

	result, err := cc.cartCollection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "删除商品失败"})
	}

	if result.ModifiedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定商品"})
	}

	return c.JSON(fiber.Map{"message": "商品已从购物车中删除"})
}

func (cc *CartController) AllfromCart(c *fiber.Ctx) error {
	// 步骤1: 验证用户并获取用户ID
	// 从JWT令牌中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		log.Println("Error: claims not found in context locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		log.Println("Error: user_id claim not found or not a string")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	// 将user_id字符串转换为primitive.ObjectID
	var userID primitive.ObjectID

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		// 如果转换失败，处理错误
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	// 步骤2: 查询购物车
	var cart models.Cart
	filter := bson.M{"user_ref": userID}
	err = cc.cartCollection.FindOne(cc.ctx, filter).Decode(&cart)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Cart not found"}) // 如果没有找到购物车或发生其他错误，返回404
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal server error"})
	}

	return c.JSON(cart)
}

func (cc *CartController) UpdatefromCart(c *fiber.Ctx) error {
	return c.SendString(" Test Route")
}
