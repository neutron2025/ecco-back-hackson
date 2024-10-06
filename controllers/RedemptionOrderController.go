package controllers

import (
	"blog-auth-server/models"
	"context"
	"log"
	"strconv"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type RedemptionOrderController struct {
	redemptionOrderCollection *mongo.Collection
	userCollection            *mongo.Collection
	orderCollection           *mongo.Collection
	ctx                       context.Context
}

func NewRedemptionOrderController(redemptionOrderCollection, userCollection, orderCollection *mongo.Collection, ctx context.Context) *RedemptionOrderController {
	return &RedemptionOrderController{
		redemptionOrderCollection: redemptionOrderCollection,
		userCollection:            userCollection,
		orderCollection:           orderCollection,
		ctx:                       ctx,
	}
}

// 个人用户创建赎回订单
func (roc *RedemptionOrderController) CreateRedemptionOrder(c *fiber.Ctx) error {

	// 从上下文中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "无法获取用户信息"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "用户ID不存在"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的用户ID"})
	}

	var input struct {
		OrderID        string `json:"order_id"`
		AlipayUsername string `json:"alipay_username"`
		AlipayAccount  string `json:"alipay_account"`
		WalletAddress  string `json:"wallet_address"`
		Hash           string `json:"hash"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的输入数据"})
	}

	// 验证订单ID
	orderID, err := primitive.ObjectIDFromHex(input.OrderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的订单ID"})
	}

	// 创建新的赎回订单
	redemptionOrder := models.RedemptionOrder{
		ID:             primitive.NewObjectID(),
		UserRef:        userID,
		OrderRef:       orderID,
		CreatedAt:      time.Now(),
		Status:         "待处理",
		IsSubmitted:    true,
		AlipayUsername: input.AlipayUsername,
		AlipayAccount:  input.AlipayAccount,
		WalletAddress:  input.WalletAddress,
		Hash:           input.Hash,
	}

	// 验证支付宝信息或钱包信息
	if (input.AlipayUsername == "" || input.AlipayAccount == "") && (input.WalletAddress == "" || input.Hash == "") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "必须提供支付宝信息或钱包信息"})
	}
	// 检查权证数量
	var order models.Orders
	err = roc.orderCollection.FindOne(c.Context(), bson.M{"_id": orderID}).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定的订单"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询订单失败"})
	}

	var user models.User
	err = roc.userCollection.FindOne(c.Context(), bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定的用户"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询用户失败"})
	}

	if order.TotalPrice > uint64(user.Pow) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "用户权证数量不足"})
	}

	// 插入数据库
	result, err := roc.redemptionOrderCollection.InsertOne(c.Context(), redemptionOrder)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建赎回订单失败"})
	}
	// 更新原始订单的 IsRedeemed 字段为 true
	_, err = roc.orderCollection.UpdateOne(
		c.Context(),
		bson.M{"_id": orderID},
		bson.M{"$set": bson.M{"is_redeemed": true}},
	)
	if err != nil {
		// 如果更新失败，可以选择回滚赎回订单的创建
		// 这里我们只记录错误，但仍然返回成功创建赎回订单的消息
		log.Printf("更新原始订单失败: %v", err)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "赎回订单创建成功",
		"id":      result.InsertedID,
	})
}

// 后端管理员获取所有赎回订单
func (roc *RedemptionOrderController) GetRedemptionOrder(c *fiber.Ctx) error {
	// 获取分页参数
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	// 确保页码和限制是正数
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	// 计算跳过的文档数
	skip := (page - 1) * limit

	// 创建一个空的赎回订单切片
	var redemptionOrders []models.RedemptionOrder

	// 设置查询选项
	opts := options.Find().
		SetSkip(int64(skip)).
		SetLimit(int64(limit)).
		SetSort(bson.D{{Key: "created_at", Value: -1}}) // 按创建时间降序排序

	// 执行查询
	cursor, err := roc.redemptionOrderCollection.Find(c.Context(), bson.M{}, opts)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "获取赎回订单失败",
		})
	}
	defer cursor.Close(c.Context())

	// 解码查询结果
	if err := cursor.All(c.Context(), &redemptionOrders); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "解码赎回订单失败",
		})
	}

	// 获取总文档数
	total, err := roc.redemptionOrderCollection.CountDocuments(c.Context(), bson.M{})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "获取总订单数失败",
		})
	}

	// 返回结果
	return c.JSON(fiber.Map{
		"orders": redemptionOrders,
		"page":   page,
		"limit":  limit,
		"total":  total,
	})
}

// 后端管理员更新赎回订单状态
func (roc *RedemptionOrderController) UpdateRedemptionOrderStatus(c *fiber.Ctx) error {
	orderID := c.Params("dempOrderID")

	var input struct {
		Status string `json:"status"`
	}

	// 解析请求体
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的输入数据"})
	}

	// 验证状态
	if input.Status == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "状态不能为空"})
	}

	// 将字符串ID转换为ObjectID
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的订单ID"})
	}

	// 更新订单状态
	update := bson.M{"$set": bson.M{"status": input.Status}}
	result, err := roc.redemptionOrderCollection.UpdateOne(
		c.Context(),
		bson.M{"_id": objectID},
		update,
	)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "更新订单状态失败"})
	}

	if result.MatchedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定的赎回订单"})
	}

	return c.JSON(fiber.Map{
		"message": "赎回订单状态更新成功",
		"status":  input.Status,
	})
}


// 根据赎回订单ID删除赎回订单
func (roc *RedemptionOrderController) DeleteRedemptionOrder(c *fiber.Ctx) error {
	orderID := c.Params("dempOrderID")

	// 将字符串ID转换为ObjectID
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的订单ID"})
	}

	// 删除赎回订单
	result, err := roc.redemptionOrderCollection.DeleteOne(c.Context(), bson.M{"_id": objectID})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "删除赎回订单失败"})
	}

	if result.DeletedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定的赎回订单"})
	}

	// 更新原始订单的 IsRedeemed 字段为 false
	_, err = roc.orderCollection.UpdateOne(
		c.Context(),
		bson.M{"_id": objectID},
		bson.M{"$set": bson.M{"is_redeemed": false}},
	)
	if err != nil {
		// 如果更新失败，记录错误但不影响删除操作的结果
		log.Printf("更新原始订单失败: %v", err)
	}

	return c.JSON(fiber.Map{
		"message": "赎回订单删除成功",
		"id":      orderID,
	})
}
