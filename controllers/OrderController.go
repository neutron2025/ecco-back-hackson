package controllers

import (
	"blog-auth-server/models"
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/dgrijalva/jwt-go"

	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gofiber/fiber/v2"
	"github.com/smartwalle/alipay/v3"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type OrderController struct {
	userCollection       *mongo.Collection // 用于操作购物车的集合
	orderCollection      *mongo.Collection // 用于操作产品的集合
	productCollection    *mongo.Collection
	cartCollection       *mongo.Collection
	addressCollection    *mongo.Collection
	statisticsCollection *mongo.Collection
	ctx                  context.Context
	alipayClient         *alipay.Client
}

// NewCartController 构造函数
func NewOrderController(userCollection, cartCollection, productCollection, orderCollection, addressCollection, statisticsCollection *mongo.Collection, ctx context.Context, alipayClient *alipay.Client) *OrderController {
	oc := &OrderController{
		userCollection:       userCollection,
		cartCollection:       cartCollection,
		productCollection:    productCollection,
		orderCollection:      orderCollection,
		addressCollection:    addressCollection,
		statisticsCollection: statisticsCollection,
		ctx:                  ctx,
		alipayClient:         alipayClient,
	}
	// 启动自动清理 goroutine
	go oc.startAutoCleanup()
	return oc
}

func (oc *OrderController) startAutoCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			oc.cleanupUnpaidOrders()
		case <-oc.ctx.Done():
			return
		}
	}
}

func (oc *OrderController) AddOrder(c *fiber.Ctx) error {
	// 从上下文中获取用户ID
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

	// 获取用户的购物车
	var cart models.Cart
	err = oc.cartCollection.FindOne(oc.ctx, bson.M{"user_ref": userID}).Decode(&cart)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "购物车为空"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "获取购物车失败"})
	}

	// 获取订单地址信息
	var orderRequest struct {
		AddressItemRef string `json:"address_item_ref"`
	}

	// 解析请求体
	if err := c.BodyParser(&orderRequest); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的请求体"})
	}

	// 检查地址ID是否为空
	if orderRequest.AddressItemRef == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "地址ID不能为空"})
	}

	// 将字符串转换为 ObjectID
	addressItemRef, err := primitive.ObjectIDFromHex(orderRequest.AddressItemRef)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的地址ID格式"})
	}

	// 现在可以使用 addressItemRef 进行后续操作
	// 验证地址是否存在
	var address models.Address
	err = oc.addressCollection.FindOne(oc.ctx, bson.M{
		"user_ref":           userID,
		"address_detail._id": addressItemRef,
	}).Decode(&address)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address"})
	}

	// 创建订单项
	var orderItems []models.OrderItem
	var totalPrice uint64 = 0

	for _, cartItem := range cart.CartItems {
		log.Printf("正在处理购物车项: ProductRef=%v, Quantity=%d", cartItem.ProductRef, cartItem.Quantity)

		// 获取产品信息
		var product models.Product
		err := oc.productCollection.FindOne(oc.ctx, bson.M{"_id": cartItem.ProductRef}).Decode(&product)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				log.Printf("未找到产品: ProductRef=%v", cartItem.ProductRef)
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到产品", "product_ref": cartItem.ProductRef})
			}
			log.Printf("获取产品信息时发生错误: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "获取产品信息失败", "details": err.Error()})
		}

		log.Printf("成功获取产品信息: ProductID=%v, Price=%d", product.ID, product.Price)

		orderItem := models.OrderItem{
			ProductRef:     cartItem.ProductRef,
			Quantity:       cartItem.Quantity,
			Size:           cartItem.Size,
			Color:          cartItem.Color,
			Price:          product.Price,
			DeliverID:      "", // 初始为空，后续可更新
			ShippingStatus: "待发货",
			AddressItemRef: addressItemRef,
		}
		orderItems = append(orderItems, orderItem)
		totalPrice += uint64(cartItem.Quantity) * product.Price
		log.Printf("添加订单项: ProductRef=%v, Quantity=%d, Price=%d", orderItem.ProductRef, orderItem.Quantity, orderItem.Price)
	}

	log.Printf("订单项处理完成，总价: %d", totalPrice)

	// // 更新所有没有 is_redeemed 字段的文档
	// result, err := oc.orderCollection.UpdateMany(
	// 	oc.ctx,
	// 	bson.M{"is_redeemed": bson.M{"$exists": false}},
	// 	bson.M{"$set": bson.M{"is_redeemed": false}},
	// )
	// if err != nil {
	// 	log.Printf("更新旧订单时发生错误: %v", err)
	// }

	// if result != nil {
	// 	log.Printf("更新的文档数: %d", result.ModifiedCount)
	// }

	/////// 更新指定用户的特定订单总金额
	// someUserID, _ := primitive.ObjectIDFromHex("66e95836bd4c2c785f8902e9")
	// someOrderID, _ := primitive.ObjectIDFromHex("66e978a9c95fb96743b7ccb8")
	
	// updateResult, err := oc.orderCollection.UpdateOne(
	// 	oc.ctx,
	// 	bson.M{"_id": someOrderID, "user_ref": someUserID},
	// 	bson.M{"$set": bson.M{"total_price": 218}},
	// )
	
	// if err != nil {
	// 	log.Printf("更新订单总金额时发生错误: %v", err)
	// 	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "更新订单总金额失败"})
	// }
	
	// if updateResult.MatchedCount == 0 {
	// 	log.Printf("未找到匹配的订单")
	// 	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定的订单"})
	// }
	
	// log.Printf("成功更新订单总金额: 用户ID=%s, 订单ID=%s, 新总金额=218", someUserID.Hex(), someOrderID.Hex())
/////////


	// 创建新订单
	newOrder := models.Orders{
		ID:            primitive.NewObjectID(),
		UserRef:       userID,
		OrderItems:    orderItems,
		TotalPrice:    totalPrice,
		Discount:      0, // 可以根据需求设置折扣
		PaymentStatus: "待支付",
		CreatedAt:     time.Now(),
		IsRedeemed:    false, // 初始化新字段
	}

	// 将订单保存到数据库
	_, err = oc.orderCollection.InsertOne(oc.ctx, newOrder)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建订单失败"})
	}

	// 清空用户的购物车  此功能放在了支付成功后
	// _, err = oc.cartCollection.UpdateOne(
	// 	oc.ctx,
	// 	bson.M{"user_ref": userID},
	// 	bson.M{"$set": bson.M{"items": []models.CartItem{}}},
	// )
	// if err != nil {
	// 	log.Printf("清空购物车失败: %v", err)
	// 	// 注意：这里我们继续处理，因为订单已经创建成功
	// }

	// 创建支付宝当面付请求
	var p = alipay.TradePreCreate{
		Trade: alipay.Trade{
			Subject:        "订单支付",
			OutTradeNo:     newOrder.ID.Hex(),
			TotalAmount:    fmt.Sprintf("%.2f", float64(newOrder.TotalPrice)), // 假设 TotalPrice 是以分为单位
			ProductCode:    "FACE_TO_FACE_PAYMENT",                            // 面对面支付的产品码
			Body:           fmt.Sprintf("订单 %s 的支付", newOrder.ID.Hex()),       // 可选：订单描述
			TimeoutExpress: "15m",                                             // 可选：订单超时时间，这里设置为15分钟
		},
	}
	rsp, err := oc.alipayClient.TradePreCreate(c.Context(), p)
	if err != nil {
		log.Printf("创建支付宝二维码失败: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建支付二维码失败"})
	}

	//打印rsp
	log.Printf("rsp: %v", rsp)
	// 将二维码链接添加到订单响应中
	orderResponse := fiber.Map{
		"order":   newOrder,
		"qr_code": rsp.QRCode,
	}

	return c.Status(fiber.StatusCreated).JSON(orderResponse)
}

// 获取个人所有订单  订单页面接口
func (oc *OrderController) GetOrder(c *fiber.Ctx) error {
	// 从上下文中获取用户ID
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
	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	// 设置分页参数
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "5"))
	skip := (page - 1) * limit

	// 创建查询过滤器
	filter := bson.M{"user_ref": userID}

	// 创建排序选项（按创建时间降序）
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetSkip(int64(skip)).SetLimit(int64(limit))

	// 执行查询
	cursor, err := oc.orderCollection.Find(oc.ctx, filter, opts)
	if err != nil {
		log.Printf("Error finding orders: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve orders"})
	}
	defer cursor.Close(oc.ctx)

	// 解码查询结果
	var orders []models.Orders
	if err = cursor.All(oc.ctx, &orders); err != nil {
		log.Printf("Error decoding orders: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to decode orders"})
	}

	// 获取订单总数（用于分页）
	total, err := oc.orderCollection.CountDocuments(oc.ctx, filter)
	if err != nil {
		log.Printf("Error counting orders: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to count orders"})
	}

	// 构建响应
	response := fiber.Map{
		"orders": orders,
		"total":  total,
		"page":   page,
		"limit":  limit,
	}

	return c.JSON(response)
}

// 获取单个订单 用户
func (oc *OrderController) GetOneOrder(c *fiber.Ctx) error {
	// 从上下文中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "未授权访问"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "无效的用户ID"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的用户ID格式"})
	}

	orderID := c.Params("orderID")
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的订单ID"})
	}

	// 创建查询过滤器，包括用户ID
	filter := bson.M{"_id": objectID, "user_ref": userID}

	var order models.Orders
	err = oc.orderCollection.FindOne(oc.ctx, filter).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "订单不存在或无权访问"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询订单失败"})
	}

	// 只返回必要的信息
	return c.JSON(fiber.Map{
		"order_id":    order.ID,
		"total_price": order.TotalPrice,
		"status":      order.PaymentStatus,
		"created_at":  order.CreatedAt,
	})
}

// 获取单个订单  管理员
func (oc *OrderController) GetOneOrderByID(c *fiber.Ctx) error {
	// 从路径参数中获取订单ID
	orderID := c.Params("orderID")

	// 验证ID格式
	if len(orderID) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "订单ID不能为空"})
	}

	// 将字符串ID转换为ObjectID
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的订单ID格式"})
	}

	// 查询订单
	var order models.Orders
	err = oc.orderCollection.FindOne(oc.ctx, bson.M{"_id": objectID}).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到指定订单"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询订单失败"})
	}

	// 查询用户信息
	var user models.User
	err = oc.userCollection.FindOne(oc.ctx, bson.M{"_id": order.UserRef}).Decode(&user)
	if err != nil {
		if err != mongo.ErrNoDocuments {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询用户信息失败"})
		}
		// 如果找不到用户，继续处理，但不包含用户信息
	}

	// 构建响应
	response := fiber.Map{
		"id":                   order.ID,
		"user_ref":             order.UserRef,
		"alipay_trade_no":      order.AlipayTradeNo,
		"order_items":          order.OrderItems,
		"total_price":          order.TotalPrice,
		"discount":             order.Discount,
		"payment_status":       order.PaymentStatus,
		"payment_time":         order.PaymentTime,
		"buyer_alipay_account": order.BuyerAlipayAccount,
		"created_at":           order.CreatedAt,
		"is_redeemed":          order.IsRedeemed,
	}

	return c.JSON(response)
}

func (oc *OrderController) CreateQRCode(c *fiber.Ctx) error {
	// 从请求中获取订单信息
	var orderInfo struct {
		OrderID string `json:"order_id"`
		Amount  string `json:"amount"`
	}
	if err := c.BodyParser(&orderInfo); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// 创建支付宝当面付请求
	var p = alipay.TradePreCreate{
		Trade: alipay.Trade{
			Subject:     "订单支付",
			OutTradeNo:  orderInfo.OrderID,
			TotalAmount: orderInfo.Amount,
			ProductCode: "FACE_TO_FACE_PAYMENT", // 面对面支付的产品码
		},
	}
	rsp, err := oc.alipayClient.TradePreCreate(c.Context(), p)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create QR code"})
	}

	// 返回二维码链接
	return c.JSON(fiber.Map{"qr_code": rsp.QRCode})
}

// 结算页面用户手动查询订单以更新
func (oc *OrderController) QueryOrder(c *fiber.Ctx) error {
	// 从上下文中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "未授权访问"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "无效的用户ID"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的用户ID格式"})
	}

	// 获取订单ID
	orderID := c.Params("orderID")
	objectID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的订单ID"})
	}

	// 查询订单，确保订单属于当前用户
	var order models.Orders
	err = oc.orderCollection.FindOne(oc.ctx, bson.M{"_id": objectID, "user_ref": userID}).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "订单不存在或无权访问"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询订单失败"})
	}

	// 如果订单状态已经是"已支付"，直接返回
	if order.PaymentStatus == "已支付" {
		return c.JSON(fiber.Map{
			"order_id":     orderID,
			"status":       order.PaymentStatus,
			"total_amount": float64(order.TotalPrice) / 100, // 假设TotalPrice以分为单位
			"pay_time":     order.PaymentTime,
		})
	}

	// 查询支付宝订单状态
	var p = alipay.TradeQuery{
		OutTradeNo: orderID,
	}
	rsp, err := oc.alipayClient.TradeQuery(c.Context(), p)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询支付宝订单失败"})
	}

	// 检查交易状态
	if rsp.TradeStatus == "TRADE_SUCCESS" {
		// 更新订单状态
		paymentTime, _ := time.Parse("2006-01-02 15:04:05", rsp.SendPayDate)
		amountFloat, _ := strconv.ParseFloat(rsp.TotalAmount, 64)
		totalAmount := uint64(math.Round(amountFloat * 100))

		update := bson.M{
			"$set": bson.M{
				"payment_status":       "已支付",
				"alipay_trade_no":      rsp.TradeNo,
				"payment_time":         paymentTime,
				"buyer_alipay_account": rsp.BuyerLogonId,
				"total_price":          totalAmount / 100,
			},
		}
		//在此处添加增加用户的积分
		powToAdd := float64(totalAmount) / 100 // 假设每消费1元增加1 Pow
		userUpdate := bson.M{
			"$inc": bson.M{"pow": powToAdd},
		}

		_, err = oc.userCollection.UpdateOne(oc.ctx, bson.M{"_id": userID}, userUpdate)
		if err != nil {
			log.Printf("更新用户Pow失败: %v", err)
			// 注意：这里我们继续处理，因为订单已经支付成功
		}

		_, err = oc.orderCollection.UpdateOne(oc.ctx, bson.M{"_id": objectID}, update)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "更新订单状态失败"})
		}

		// 清空用户的购物车
		_, err = oc.cartCollection.UpdateOne(
			oc.ctx,
			bson.M{"user_ref": userID},
			bson.M{"$set": bson.M{"items": []models.CartItem{}}},
		)
		if err != nil {
			log.Printf("清空购物车失败: %v", err)
		}

		return c.JSON(fiber.Map{
			"message":      "订单已支付",
			"order_id":     orderID,
			"trade_status": rsp.TradeStatus,
			"total_amount": float64(totalAmount) / 100,
			"pay_time":     paymentTime,
		})
	}

	// 如果订单状态不是 TRADE_SUCCESS，返回当前状态
	return c.JSON(fiber.Map{
		"order_id":     orderID,
		"trade_status": rsp.TradeStatus,
		"total_amount": rsp.TotalAmount,
		"message":      "订单尚未支付",
	})
}

// 用户主页自动查询订单
func (oc *OrderController) QueryOrderAuto(c *fiber.Ctx) error {
	// 从上下文中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "未授权访问"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "无效的用户ID"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的用户ID格式"})
	}

	// 查询用户的所有待支付订单
	filter := bson.M{"user_ref": userID, "payment_status": "待支付"}
	cursor, err := oc.orderCollection.Find(oc.ctx, filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询订单失败"})
	}
	defer cursor.Close(oc.ctx)

	var orders []models.Orders
	if err = cursor.All(oc.ctx, &orders); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "解析订单失败"})
	}

	// 检查是否有待支付订单
	if len(orders) == 0 {
		return c.JSON(fiber.Map{
			"message":        "没有待支付的订单",
			"updated_orders": []fiber.Map{},
		})
	}

	updatedOrders := []fiber.Map{}

	for _, order := range orders {
		// 查询支付宝订单状态
		var p = alipay.TradeQuery{
			OutTradeNo: order.ID.Hex(),
		}
		rsp, err := oc.alipayClient.TradeQuery(c.Context(), p)
		if err != nil {
			log.Printf("查询支付宝订单失败 (OrderID: %s): %v", order.ID.Hex(), err)
			continue
		}

		// 检查交易状态
		if rsp.TradeStatus == "TRADE_SUCCESS" {
			// 更新订单状态
			paymentTime, _ := time.Parse("2006-01-02 15:04:05", rsp.SendPayDate)
			amountFloat, _ := strconv.ParseFloat(rsp.TotalAmount, 64)
			totalAmount := uint64(math.Round(amountFloat * 100))

			update := bson.M{
				"$set": bson.M{
					"payment_status":       "已支付",
					"alipay_trade_no":      rsp.TradeNo,
					"payment_time":         paymentTime,
					"buyer_alipay_account": rsp.BuyerLogonId,
					"total_price":          totalAmount / 100,
				},
			}

			// 更新订单
			_, err = oc.orderCollection.UpdateOne(oc.ctx, bson.M{"_id": order.ID}, update)
			if err != nil {
				log.Printf("更新订单状态失败 (OrderID: %s): %v", order.ID.Hex(), err)
				continue
			}

			// 增加用户的积分
			powToAdd := float64(totalAmount) / 100 // 假设每消费1元增加1 Pow
			userUpdate := bson.M{
				"$inc": bson.M{"pow": powToAdd},
			}

			_, err = oc.userCollection.UpdateOne(oc.ctx, bson.M{"_id": userID}, userUpdate)
			if err != nil {
				log.Printf("更新用户Pow失败 (UserID: %s): %v", userID.Hex(), err)
				// 注意：这里我们继续处理，因为订单已经更新成功
			}

			updatedOrders = append(updatedOrders, fiber.Map{
				"order_id":     order.ID.Hex(),
				"status":       "已支付",
				"total_amount": float64(totalAmount) / 100,
				"pay_time":     paymentTime,
			})
		}
	}

	// 根据更新结果返回相应的消息
	if len(updatedOrders) > 0 {
		return c.JSON(fiber.Map{
			"message":        "自动查询订单完成，部分订单已更新",
			"updated_orders": updatedOrders,
		})
	} else {
		return c.JSON(fiber.Map{
			"message":        "自动查询订单完成，没有订单状态需要更新",
			"updated_orders": []fiber.Map{},
		})
	}
}

// 后台导出订单到excelExportOrders
func (oc *OrderController) ExportOrders(c *fiber.Ctx) error {
	f := excelize.NewFile()
	sheetName := "订单详情"
	_, err := f.NewSheet(sheetName)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建工作表失败"})
	}

	// 设置列标题
	titles := []string{"订单ID", "用户ID", "支付状态", "支付时间", "创建时间",
		"商品ID", "商品数量", "商品尺寸", "商品颜色",
		"收件人姓名", "收件人电话", "收件地址"}
	for i, title := range titles {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, title)
	}

	// 查询已支付的订单
	cursor, err := oc.orderCollection.Find(oc.ctx, bson.M{"payment_status": "已支付"})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "查询订单失败"})
	}
	defer cursor.Close(oc.ctx)

	var orders []models.Orders
	if err = cursor.All(oc.ctx, &orders); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "解析订单失败"})
	}

	// 检查是否有已支付的订单
	if len(orders) == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "没有找到已支付的订单"})
	}

	row := 2
	for _, order := range orders {
		for _, item := range order.OrderItems {
			// 使用 AddressItemRef 查询地址信息
			var address models.Address
			err := oc.addressCollection.FindOne(
				oc.ctx,
				bson.M{"address_detail._id": item.AddressItemRef},
			).Decode(&address)

			var targetAddress models.AddressItem
			if err != nil {
				log.Printf("查询地址信息失败: OrderID=%s, ItemID=%s, AddressID=%s, Error=%v",
					order.ID.Hex(), item.ProductRef.Hex(), item.AddressItemRef.Hex(), err)
				// 使用默认值
				targetAddress = models.AddressItem{
					FirstName: "未知",
					LastName:  "未知",
					Phone:     "未知",
					Street:    "未知",
					City:      "未知",
					State:     "未知",
					ZipCode:   "未知",
				}
			} else {
				// 在地址列表中查找匹配的地址项
				for _, addr := range address.AddressDetails {
					if addr.ID == item.AddressItemRef {
						targetAddress = addr
						break
					}
				}
				if targetAddress.ID.IsZero() {
					log.Printf("未找到匹配的地址项: OrderID=%s, ItemID=%s, AddressID=%s",
						order.ID.Hex(), item.ProductRef.Hex(), item.AddressItemRef.Hex())
					// 使用默认值
					targetAddress = models.AddressItem{
						FirstName: "未知",
						LastName:  "未知",
						Phone:     "未知",
						Street:    "未知",
						City:      "未知",
						State:     "未知",
						ZipCode:   "未知",
					}
				}
			}

			// 设置单元格值
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), order.ID.Hex())
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), order.UserRef.Hex())
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), order.PaymentStatus)
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), order.PaymentTime.Format("2006-01-02 15:04:05"))
			f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), order.CreatedAt.Format("2006-01-02 15:04:05"))
			f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), item.ProductRef.Hex())
			f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), item.Quantity)
			f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), item.Size)
			f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), item.Color)
			f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), targetAddress.FirstName+" "+targetAddress.LastName)
			f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), targetAddress.Phone)
			f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("%s, %s, %s %s", targetAddress.State, targetAddress.City, targetAddress.Street, targetAddress.ZipCode))

			row++
		}
	}
	// 设置活动工作表
	sheetIndex, err := f.GetSheetIndex(sheetName)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "设置活动工作表失败"})
	}
	f.SetActiveSheet(sheetIndex)

	// 生成文件名和设置响应头
	currentTime := time.Now()
	fileName := fmt.Sprintf("OrderExport_%s.xlsx", currentTime.Format("20060102_1504"))
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))

	tempFile := path.Join(os.TempDir(), fileName)
	if err := f.SaveAs(tempFile); err != nil {
		log.Printf("保存临时文件失败: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "生成 Excel 文件失败"})
	}
	defer os.Remove(tempFile)

	return c.Download(tempFile, fileName)
	// // 将文件写入响应
	// if err := f.Write(c.Response().BodyWriter()); err != nil {
	// 	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "生成 Excel 文件失败"})
	// }
	// log.Printf("成功生成文件: %s", fileName)
	// return nil
}

// GetAllOrders 查询所有订单
// GET /orders?page=1&limit=20
func (oc *OrderController) GetAllOrders(c *fiber.Ctx) error {
	// 获取分页参数
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	skip := (page - 1) * limit

	// 获取排序参数
	sortField := c.Query("sort_by", "created_at")
	sortOrder := c.Query("sort_order", "asc") // 默认为降序
	sortValue := 1                            // 默认为降序
	if sortOrder == "desc" {
		sortValue = -1
	}

	// 创建查询过滤器
	filter := bson.M{}

	// // 添加支付状态筛选
	// if paymentStatus := c.Query("payment_status"); paymentStatus != "" {
	// 	filter["PaymentStatus"] = paymentStatus
	// }
	// log.Printf("查询条件: %+v", filter)

	// 创建查询选项
	opts := options.Find().
		SetSort(bson.D{{Key: sortField, Value: sortValue}}).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))

	// 执行查询
	cursor, err := oc.orderCollection.Find(oc.ctx, filter, opts)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "查询订单失败",
		})
	}
	defer cursor.Close(oc.ctx)

	// 解码查询结果
	var orders []models.Orders
	if err = cursor.All(oc.ctx, &orders); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "解码订单数据失败",
		})
	}
	// // 执行查询后
	// log.Printf("查询到 %d 个订单", len(orders))

	// 获取总订单数
	total, err := oc.orderCollection.CountDocuments(oc.ctx, filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "获取订单总数失败",
		})
	}

	// 构建响应
	return c.JSON(fiber.Map{
		"orders":      orders,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": int(math.Ceil(float64(total) / float64(limit))),
	})
}

// 更新订单项发货状态
func (oc *OrderController) UpdateOrderItemShippingStatus(c *fiber.Ctx) error {
	// 从请求中获取订单ID、商品ID和新状态
	var updateInfo struct {
		OrderID   string `json:"order_id"`
		ProductID string `json:"product_id"`
		Status    string `json:"status"`
	}

	if err := c.BodyParser(&updateInfo); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "无效的请求数据",
		})
	}

	// log.Printf("接收到的更新信息: OrderID=%s, ProductID=%s, Status=%s", updateInfo.OrderID, updateInfo.ProductID, updateInfo.Status)

	// 验证订单ID格式
	orderID, err := primitive.ObjectIDFromHex(updateInfo.OrderID)
	if err != nil {
		log.Printf("无效的订单ID: %s, 错误: %v", updateInfo.OrderID, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "无效的订单ID",
		})
	}

	// 验证商品ID格式
	productID, err := primitive.ObjectIDFromHex(updateInfo.ProductID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "无效的商品ID",
		})
	}

	// 更新订单项的发货状态
	filter := bson.M{"_id": orderID, "items.product_ref": productID}
	update := bson.M{"$set": bson.M{"items.$[elem].shipping_status": updateInfo.Status}}

	// 使用 arrayFilters 来匹配所有符合条件的元素
	opts := options.Update().SetArrayFilters(options.ArrayFilters{
		Filters: []interface{}{bson.M{"elem.product_ref": productID}},
	})

	result, err := oc.orderCollection.UpdateOne(oc.ctx, filter, update, opts)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "更新发货状态失败",
		})
	}

	if result.MatchedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "未找到指定订单或商品",
		})
	}

	return c.JSON(fiber.Map{
		"message":    "发货状态已更新",
		"order_id":   updateInfo.OrderID,
		"product_id": updateInfo.ProductID,
		"new_status": updateInfo.Status,
	})
}

// 更新订单项快递单号
func (oc *OrderController) UpdateOrderItemDeliverID(c *fiber.Ctx) error {
	// 从请求中获取订单ID、商品ID和快递单号
	var updateInfo struct {
		OrderID   string `json:"order_id"`
		ProductID string `json:"product_id"`
		DeliverID string `json:"deliver_id"`
	}
	if err := c.BodyParser(&updateInfo); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "无效的请求数据",
		})
	}

	log.Printf("接收到的更新信息: OrderID=%s, ProductID=%s, DeliverID=%s", updateInfo.OrderID, updateInfo.ProductID, updateInfo.DeliverID)

	// 验证订单ID格式
	orderID, err := primitive.ObjectIDFromHex(updateInfo.OrderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "无效的订单ID",
		})
	}

	// 验证商品ID格式
	productID, err := primitive.ObjectIDFromHex(updateInfo.ProductID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "无效的商品ID",
		})
	}

	// 更新订单项的快递单号
	filter := bson.M{"_id": orderID, "items.product_ref": productID}
	update := bson.M{"$set": bson.M{"items.$[elem].deliver_id": updateInfo.DeliverID}}
	// 使用 arrayFilters 来匹配所有符合条件的元素
	opts := options.Update().SetArrayFilters(options.ArrayFilters{
		Filters: []interface{}{bson.M{"elem.product_ref": productID}},
	})

	result, err := oc.orderCollection.UpdateOne(oc.ctx, filter, update, opts)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "更新快递单号失败",
		})
	}

	if result.MatchedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "未找到指定订单或商品",
		})
	}

	return c.JSON(fiber.Map{
		"message":    "快递单号已更新",
		"order_id":   updateInfo.OrderID,
		"product_id": updateInfo.ProductID,
		"deliver_id": updateInfo.DeliverID,
	})
}

// getOrderItemDeliverID 获取订单项快递单号
func (oc *OrderController) GetOrderItemDeliverID(c *fiber.Ctx) error {
	// 从请求中获取订单ID和商品ID
	var queryParams struct {
		OrderID   string `query:"order_id"`
		ProductID string `query:"product_id"`
	}

	if err := c.QueryParser(&queryParams); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid query parameters"})
	}

	// 验证订单ID格式
	orderID, err := primitive.ObjectIDFromHex(queryParams.OrderID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid order ID"})
	}

	// 验证商品ID格式
	productID, err := primitive.ObjectIDFromHex(queryParams.ProductID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid product ID"})
	}

	// 查询订单项
	filter := bson.M{"_id": orderID, "items.product_ref": productID}
	var order models.Orders
	err = oc.orderCollection.FindOne(oc.ctx, filter).Decode(&order)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Order item not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get order item"})
	}

	// 返回订单项的快递单号
	for _, item := range order.OrderItems {
		if item.ProductRef == productID {
			return c.JSON(fiber.Map{"deliver_id": item.DeliverID})
		}
	}

	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Order item not found"})
}

// 定时自动清除未支付订单
func (oc *OrderController) cleanupUnpaidOrders() {
	filter := bson.M{
		"$or": []bson.M{
			{
				"payment_status": "待支付",
				"created_at": bson.M{
					"$ne": time.Time{},
					"$lt": time.Now().Add(-15 * time.Minute),
				},
				"total_price": bson.M{"$gt": 0},
			},
			{
				"$or": []bson.M{
					{"payment_status": ""},
					{"created_at": time.Time{}},
					{"total_price": 0},
					{"items": bson.M{"$size": 0}},
				},
			},
		},
	}

	// 首先检查是否有需要清理的订单
	count, err := oc.orderCollection.CountDocuments(oc.ctx, filter)
	if err != nil {
		log.Printf("检查待清理订单数量失败: %v", err)
		return
	}

	if count == 0 {
		// 如果没有需要清理的订单，直接返回
		log.Println("自动清理: 没有需要清理的订单")
		return
	}

	var totalAmountSaved int64 // 改用 int64 来存储总金额（单位：元）
	var orders []models.Orders
	cursor, err := oc.orderCollection.Find(oc.ctx, filter)
	if err == nil {
		if err = cursor.All(oc.ctx, &orders); err == nil {
			for _, order := range orders {
				totalAmountSaved += int64(order.TotalPrice) // 直接累加，不进行单位转换
			}
		}
	}

	result, err := oc.orderCollection.DeleteMany(oc.ctx, filter)
	if err != nil {
		log.Printf("自动删除未支付订单失败: %v", err)
		return
	}

	// 在日志输出时进行单位转换
	log.Printf("自动清理: 删除了 %d 个订单, 总金额: %.2f 元", result.DeletedCount, float64(totalAmountSaved))

	stats := models.OrderCleanupStatistics{
		CleanupDate:      time.Now(),
		DeletedCount:     result.DeletedCount,
		TotalAmountSaved: float64(totalAmountSaved), // 存储到数据库时转换为元
	}

	_, err = oc.statisticsCollection.InsertOne(oc.ctx, stats)
	if err != nil {
		log.Printf("保存清理统计数据失败: %v", err)
	}
}

// 展示后台销售数据GetSales
func (oc *OrderController) GetSales(c *fiber.Ctx) error {

	return c.JSON(fiber.Map{
		"message": "展示后台销售数据",
	})
}

// 展示后台浏览数据GetVisitors
func (oc *OrderController) GetVisitors(c *fiber.Ctx) error {

	return c.JSON(fiber.Map{
		"message": "展示后台浏览数据",
	})
}

// 展示后台销售数据分析GetSalesAnalytics
func (oc *OrderController) GetSalesAnalytics(c *fiber.Ctx) error {

	return c.JSON(fiber.Map{
		"message": "展示后台销售数据分析",
	})
}

// 展示后台浏览数据分析GetVisitorsAnalytics
func (oc *OrderController) GetVisitorsAnalytics(c *fiber.Ctx) error {

	return c.JSON(fiber.Map{
		"message": "展示后台浏览数据分析",
	})
}

// 提现权证
var (
	sclTokenMint   = "5iSZFcoi4NRqGPDH6hLxEyimczqr4i7ynS3aG21GPNTQ"
	fromPrivateKey = "solana-wallet-private-key"
	// sclTokenMint   = os.Getenv("SCL_TOKEN_MINT")   //您的SCL代币合约地址
	// fromPrivateKey = os.Getenv("FROM_PRIVATE_KEY") //您的私钥
)

// 节点组
var rpcEndpoints = []string{
	"https://solana-mainnet.core.chainstack.com/f87d916aa1ef3bcc218e997ddf99ea27",
	"https://go.getblock.io/d554a8ea22e744c49d390bd6b1fb542d",
	"https://go.getblock.io/7d204ffa73a943ddbbe6de3ecae453e2",
	"https://go.getblock.io/ab67f11ec194454f996254c3e3042292",
}

// 轮询获取下一个 RPC 端点
var currentRPCIndex = 0

func getNextRPCEndpoint() string {
	endpoint := rpcEndpoints[currentRPCIndex]
	currentRPCIndex = (currentRPCIndex + 1) % len(rpcEndpoints)
	return endpoint
}

// 创建一个限流器，每秒允许5个请求
var limiter = rate.NewLimiter(rate.Every(time.Second), 5)

// 等待函数，用于在发送请求前调用
func waitForRateLimit(ctx context.Context) error {
	return limiter.Wait(ctx)
}

// ATA创建 原子锁
var ataCreationMutex sync.Mutex

func createATAIfNeeded(client *rpc.Client, fromAccount solana.PrivateKey, toPublicKey solana.PublicKey) error {
	ataCreationMutex.Lock()
	defer ataCreationMutex.Unlock()
	ctx := context.Background()

	// 检查 ATA 是否存在
	toTokenAccount, _, err := solana.FindAssociatedTokenAddress(toPublicKey, solana.MustPublicKeyFromBase58(sclTokenMint))
	if err != nil {
		return fmt.Errorf("获取接收者代币账户失败: %v", err)
	}

	_, err = client.GetAccountInfo(context.Background(), toTokenAccount)
	if err == nil {
		// ATA 已存在
		return nil
	}

	// 创建 ATA
	// 如果账户不存在，创建它
	recent, err := client.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("获取最新区块哈希失败: %v", err)
	}

	createATAInstruction := associatedtokenaccount.NewCreateInstruction(
		fromAccount.PublicKey(),
		toPublicKey,
		solana.MustPublicKeyFromBase58(sclTokenMint),
	).Build()

	// 创建并发送创建 ATA 的交易
	createATATx, err := solana.NewTransaction(
		[]solana.Instruction{createATAInstruction},
		recent.Value.Blockhash,
		solana.TransactionPayer(fromAccount.PublicKey()),
	)
	if err != nil {
		return fmt.Errorf("创建 ATA 交易失败: %v", err)
	}
	// 签名并发送创建 ATA 的交易
	createATATx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(fromAccount.PublicKey()) {
			return &fromAccount
		}
		return nil
	})
	if err := waitForRateLimit(ctx); err != nil {
		return fmt.Errorf("等待限流失败: %v", err)
	}

	createATASig, err := client.SendTransactionWithOpts(ctx, createATATx,
		rpc.TransactionOpts{
			SkipPreflight:       false,
			PreflightCommitment: rpc.CommitmentFinalized,
		},
	)
	if err != nil {
		return fmt.Errorf("发送创建 ATA 交易失败: %v", err)
	}

	log.Printf("已发送创建 ATA 交易，签名: %s", createATASig)
	// 等待 ATA 创建交易确认
	if err := waitForConfirmation(client, createATASig, 20); err != nil {
		return fmt.Errorf("等待 ATA 创建确认失败: %v", err)
	}
	log.Printf("ATA 创建成功")
	// 额外等待一段时间，确保 ATA 完全生效
	time.Sleep(5 * time.Second)

	// 再次检查 ATA 是否存在
	_, err = client.GetAccountInfo(ctx, toTokenAccount)
	if err != nil {
		return fmt.Errorf("ATA 创建后仍无法检测到: %v", err)
	}

	return nil

}

// 重试方法   SCL 和 SOL 的转账  都用
func retryWithExponentialBackoff(operation func() error) error {
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}
		if attempt == maxRetries-1 {
			return err
		}
		backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		jitter := time.Duration(rand.Int63n(int64(backoffDuration) / 2))
		backoffDuration += jitter
		time.Sleep(backoffDuration)
	}
	return fmt.Errorf("达到最大重试次数")
}

// 异步转账 SCL
func transferSCLAsync(client *rpc.Client, fromAccount solana.PrivateKey, toPublicKey solana.PublicKey, amount float64) chan error {
	resultChan := make(chan error, 1)

	go func() {
		err := retryWithExponentialBackoff(func() error {
			ctx := context.Background()

			// 获取代币账户
			fromTokenAccount, _, err := solana.FindAssociatedTokenAddress(fromAccount.PublicKey(), solana.MustPublicKeyFromBase58(sclTokenMint))
			if err != nil {
				return fmt.Errorf("获取发送者代币账户失败: %v", err)
			}

			toTokenAccount, _, err := solana.FindAssociatedTokenAddress(toPublicKey, solana.MustPublicKeyFromBase58(sclTokenMint))
			if err != nil {
				return fmt.Errorf("获取接收者代币账户失败: %v", err)
			}

			// 使用 createATAIfNeeded 函数来检查和创建 ATA
			if err := createATAIfNeeded(client, fromAccount, toPublicKey); err != nil {
				return fmt.Errorf("创建 ATA 失败: %v", err)
			}

			// 创建转账指令
			transferInstruction := token.NewTransferCheckedInstruction(
				uint64(amount*1e2), // SCL 的精度是 2
				2,
				fromTokenAccount,
				solana.MustPublicKeyFromBase58(sclTokenMint),
				toTokenAccount,
				fromAccount.PublicKey(),
				[]solana.PublicKey{},
			).Build()

			// 创建交易
			recent, err := client.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
			if err != nil {
				return fmt.Errorf("获取最新区块哈希失败: %v", err)
			}
			tx, err := solana.NewTransaction(
				[]solana.Instruction{transferInstruction},
				recent.Value.Blockhash,
				solana.TransactionPayer(fromAccount.PublicKey()),
			)
			if err != nil {
				return fmt.Errorf("创建交易失败: %v", err)
			}

			// 签名交易
			tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
				if key.Equals(fromAccount.PublicKey()) {
					return &fromAccount
				}
				return nil
			})

			// 在发送交易之前等待限流
			if err := waitForRateLimit(ctx); err != nil {
				return fmt.Errorf("等待限流失败: %v", err)
			}

			// 发送交易并等待确认
			sig, err := client.SendTransactionWithOpts(ctx, tx,
				rpc.TransactionOpts{
					SkipPreflight:       false,
					PreflightCommitment: rpc.CommitmentFinalized,
				},
			)
			if err != nil {
				return fmt.Errorf("发送交易失败: %v", err)
			}

			// 等待交易确认
			for i := 0; i < 20; i++ { // 尝试20次
				time.Sleep(3 * time.Second)
				statuses, err := client.GetSignatureStatuses(
					ctx,
					true, // 搜索历史记录
					sig,  // 传入单个签名
				)
				if err != nil {
					continue
				}
				if len(statuses.Value) > 0 && statuses.Value[0] != nil {
					if statuses.Value[0].Confirmations != nil && *statuses.Value[0].Confirmations > 0 {
						return nil
					}
				}
			}
			return fmt.Errorf("交易确认超时")
		})
		resultChan <- err
	}()

	return resultChan
}

// 用于ATA等待交易确认
func waitForConfirmation(client *rpc.Client, sig solana.Signature, maxAttempts int) error {
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(3 * time.Second)
		statuses, err := client.GetSignatureStatuses(
			context.Background(),
			true,
			sig,
		)
		if err != nil {
			continue
		}
		if len(statuses.Value) > 0 && statuses.Value[0] != nil {
			if statuses.Value[0].Err != nil {
				return fmt.Errorf("交易失败: %v", statuses.Value[0].Err)
			}
			if statuses.Value[0].Confirmations != nil && *statuses.Value[0].Confirmations > 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("交易确认超时")
}

///////下面是SOL的转账

// 异步转账 SOL
func transferSOLAsync(client *rpc.Client, fromAccount solana.PrivateKey, toPublicKey solana.PublicKey, amount float64) chan error {
	resultChan := make(chan error, 1)

	go func() {
		err := retryWithExponentialBackoff(func() error {
			ctx := context.Background()

			// 创建转账指令
			transferInstruction := system.NewTransferInstruction(
				uint64(amount*float64(solana.LAMPORTS_PER_SOL)),
				fromAccount.PublicKey(),
				toPublicKey,
			).Build()

			// 创建交易
			recent, err := client.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
			if err != nil {
				return fmt.Errorf("获取最新区块哈希失败: %v", err)
			}

			tx, err := solana.NewTransaction(
				[]solana.Instruction{transferInstruction},
				recent.Value.Blockhash,
				solana.TransactionPayer(fromAccount.PublicKey()),
			)
			if err != nil {
				return fmt.Errorf("创建交易失败: %v", err)
			}

			// 签名交易
			tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
				if key.Equals(fromAccount.PublicKey()) {
					return &fromAccount
				}
				return nil
			})

			// 在发送交易之前等待限流
			if err := waitForRateLimit(ctx); err != nil {
				return fmt.Errorf("等待限流失败: %v", err)
			}

			// 发送交易并等待确认
			sig, err := client.SendTransactionWithOpts(ctx, tx,
				rpc.TransactionOpts{
					SkipPreflight:       false,
					PreflightCommitment: rpc.CommitmentFinalized,
				},
			)
			if err != nil {
				return fmt.Errorf("发送交易失败: %v", err)
			}

			// 等待交易确认
			for i := 0; i < 20; i++ {
				time.Sleep(3 * time.Second)
				statuses, err := client.GetSignatureStatuses(
					ctx,
					true,
					sig,
				)
				if err != nil {
					continue
				}
				if len(statuses.Value) > 0 && statuses.Value[0] != nil {
					if statuses.Value[0].Confirmations != nil && *statuses.Value[0].Confirmations > 0 {
						return nil
					}
				}
			}
			return fmt.Errorf("交易确认超时")
		})
		resultChan <- err
	}()

	return resultChan
}

// 检查接收方 SOL 余额，新账户创建并转账，有sol的账户不创建不转账
func checkAndTransferSOL(client *rpc.Client, fromAccount solana.PrivateKey, toPublicKey solana.PublicKey) (bool, error) {

	// 获取接收方账户信息
	accountInfo, err := client.GetAccountInfo(context.Background(), toPublicKey)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return false, fmt.Errorf("获取接收方账户信息失败: %v", err)
	}

	// 计算最小所需余额
	minBalance, err := getMinimumBalanceWithBuffer(client)
	if err != nil {
		return false, fmt.Errorf("计算最小余额失败: %v", err)
	}

	var transferAmount float64
	if accountInfo == nil || accountInfo.Value == nil {
		// 账户不存在，转入最小余额
		transferAmount = minBalance
		fmt.Printf("接收方账户不存在，将创建账户并转入 %f SOL\n", transferAmount)
	} else if accountInfo.Value.Lamports < uint64(minBalance*1e9) {
		// 账户存在但余额不足，补足差额
		transferAmount = minBalance - float64(accountInfo.Value.Lamports)/1e9
		fmt.Printf("接收方余额不足，需要转入 %f SOL\n", transferAmount)
	} else {
		fmt.Printf("接收方余额充足，无需转账 SOL\n")
		return false, nil // 不需要转账
	}
	fmt.Printf("准备转账，发送方: %s, 接收方: %s, 金额: %f SOL\n",
		fromAccount.PublicKey(), toPublicKey, transferAmount)

	// 异步转账 SOL
	solResultChan := transferSOLAsync(client, fromAccount, toPublicKey, transferAmount)

	// 等待转账结果
	select {
	case err := <-solResultChan:
		if err != nil {
			return false, fmt.Errorf("SOL 转账失败: %v", err)
		}
		return true, nil // 转账成功
	case <-time.After(30 * time.Second):
		return false, fmt.Errorf("SOL 转账超时")
	}
}

// 获取最小余额
func getMinimumBalanceWithBuffer(client *rpc.Client) (float64, error) {
	minBalance, err := client.GetMinimumBalanceForRentExemption(
		context.Background(),
		0,
		rpc.CommitmentConfirmed,
	)
	if err != nil {
		return 0, err
	}
	// 转换为 SOL 并添加小额缓冲
	return float64(minBalance)/1e9 + 0.00005, nil
}

// 提现权证
func (oc *OrderController) TransferSCL(c *fiber.Ctx) error {
	var req struct {
		Amount float64 `json:"amount"`
	}

	if err := c.BodyParser(&req); err != nil {
		log.Printf("解析请求体失败: %v", err)

		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的请求体"})
	}
	// 添加一些调试日志
	log.Printf("收到的提现请求: %+v", req)

	// 验证提现数量
	if req.Amount < 218 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"message": "提现数量不得少于 218"})
	}

	// 从上下文中获取用户ID
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "无法获取用户信息"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "无法获取用户ID"})
	}

	// 查询用户信息以获取 PowAddress 和 Pow 余额
	var user models.User
	userID, _ := primitive.ObjectIDFromHex(userIDStr)
	err := oc.userCollection.FindOne(oc.ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "获取用户信息失败"})
	}

	if user.PowAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"message": "用户未设置权证地址"})
	}
	// 检查用户余额是否足够
	if user.Pow < req.Amount {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"message": "余额不足"})
	}

	// 创建 Solana 客户端，使用轮询的 RPC 节点
	sclEndpoint := getNextRPCEndpoint()
	sclClient := rpc.New(sclEndpoint)

	solEndpoint := getNextRPCEndpoint()
	solClient := rpc.New(solEndpoint)

	// 解析私钥（这应该是您的服务账户的私钥）
	// log.Printf("解析私钥-: %v", fromPrivateKey)
	log.Printf("SCL 节点--: %v", sclEndpoint)
	log.Printf("SOL 节点--: %v", solEndpoint)
	log.Printf("代币---: %v", sclTokenMint)

	fromAccount, err := solana.PrivateKeyFromBase58(fromPrivateKey)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "解析私钥失败"})
	}

	// 解析用户的 PowAddress
	toPublicKey, err := solana.PublicKeyFromBase58(user.PowAddress)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的用户权证地址"})
	}

	// 检查并转账 SOL（如果需要）
	solTransferred, err := checkAndTransferSOL(solClient, fromAccount, toPublicKey)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "SOL 转账检查失败: " + err.Error()})
	}
	// 异步转账 SCL
	sclResultChan := transferSCLAsync(sclClient, fromAccount, toPublicKey, req.Amount)

	// 设置超时和等待 SCL 转账结果
	select {
	case err := <-sclResultChan:
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "SCL 转账失败: " + err.Error()})
		}
		// SCL 转账成功，更新用户的 Pow 余额
		_, updateErr := oc.userCollection.UpdateOne(
			oc.ctx,
			bson.M{"_id": userID},
			bson.M{"$inc": bson.M{"pow": -req.Amount}},
		)
		if updateErr != nil {
			// 记录错误，但不影响转账结果
			log.Printf("更新用户 Pow 余额失败: %v", updateErr)
		}
	case <-time.After(2 * time.Minute):
		return c.Status(fiber.StatusRequestTimeout).JSON(fiber.Map{"error": "SCL 转账超时"})
	}

	// 根据是否转账 SOL 返回不同的消息
	if solTransferred {
		return c.JSON(fiber.Map{"message": "SCL 转账成功，同时转入了少量 SOL 作为手续费"})
	} else {
		return c.JSON(fiber.Map{"message": "SCL 转账成功"})
	}

}

// 赎回权证
func estimateUSDCTransferFee(client *rpc.Client, receiver solana.PublicKey, usdcMint solana.PublicKey) (float64, error) {
	// 检查接收方是否已有 USDC 账户
	ata, _, _ := solana.FindAssociatedTokenAddress(receiver, usdcMint)
	_, err := client.GetAccountInfo(context.Background(), ata)

	if err != nil {
		// 需要创建新账户
		minBalance, err := client.GetMinimumBalanceForRentExemption(context.Background(), 0, rpc.CommitmentConfirmed)
		if err != nil {
			return 0, err
		}
		return float64(minBalance)/1e9 + 0.000005, nil // 账户创建费 + 交易费
	}

	// 只需支付交易费
	return 0.000005, nil
}

func (oc *OrderController) RedeemPow(c *fiber.Ctx) error {

	return c.JSON(fiber.Map{
		"message": "赎回权证",
	})
}
