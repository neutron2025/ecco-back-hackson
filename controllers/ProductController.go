package controllers

import (
	"blog-auth-server/models"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// var SecretKey = []byte("SecretKey")

type ProductController struct {
	collection *mongo.Collection
	ctx        context.Context
}

func NewProductController(collection *mongo.Collection, ctx context.Context) *ProductController {
	return &ProductController{
		collection: collection,
		ctx:        ctx,
	}
}
func generateTimestampFilename(originalFilename string) string {
	ext := filepath.Ext(originalFilename)
	name := strings.TrimSuffix(originalFilename, ext)
	timestamp := time.Now().Format("20060102150405") //按照20060102150405 这种格式格式化时间
	return fmt.Sprintf("%s_%s%s", name, timestamp, ext)
}
func (pc *ProductController) AddProduct(c *fiber.Ctx) error {
	// 创建一个新的Product变量
	var product models.Product

	// 解析请求体到product结构体中
	if err := c.BodyParser(&product); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to parse request body"})
	}

	// 进行数据验证
	if product.Name == "" || product.Description == "" || product.Price == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing required product information"})
	}

	product.ID = primitive.NewObjectID()
	product.CreatedAt = time.Now()
	// 可以添加更多的验证逻辑，例如检查价格是否为正数、库存是否有效等

	// form上传
	mainImageURL := ""
	colorVariantImages := make([]models.Image, 0)
	introductoryImages := make([]models.Image, 0)

	form, err := c.MultipartForm()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Error retrieving uploaded files"})
	}

	// 初始化 sizeColors 切片
	sizeColors := []models.SizeColor{}

	// 由于前端使用索引来区分尺寸和颜色组，我们需要找到最大的尺寸索引
	sizeIndex := 0
	for {
		sizeKey := fmt.Sprintf("size_colors[%d][size]", sizeIndex)
		if _, exists := form.Value[sizeKey]; !exists {
			break
		}
		sizeValue := form.Value[sizeKey][0] // 取第一个元素作为尺寸值

		// 获取与当前尺寸对应的颜色列表
		colorKey := fmt.Sprintf("size_colors[%d][colors][]", sizeIndex)
		colorValues := form.Value[colorKey]

		// 将颜色列表从字符串数组转换为字符串切片
		colors := make([]string, len(colorValues))
		for i, color := range colorValues {
			colors[i] = color
		}

		// 创建 SizeColor 结构并添加到 sizeColors 切片
		sizeColors = append(sizeColors, models.SizeColor{
			Size:   sizeValue,
			Colors: colors,
		})
		sizeIndex++
	}

	// 将提取的尺寸和颜色数据赋值给 product 结构体的相应字段
	product.SizeColors = sizeColors

	// 处理图片上传
	// 处理主图
	mainFiles := form.File["main_image"]
	if len(mainFiles) > 0 {
		mainFile := mainFiles[0]
		log.Println("Main image filename:", mainFile.Filename)
		src, err := mainFile.Open()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error opening main image file"})
		}
		defer src.Close()

		if err := os.MkdirAll("upload", os.ModePerm); err != nil {

			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error creating upload directory"})
		}

		// 生成带时间戳的唯一文件名
		uniqueFilename := generateTimestampFilename(mainFile.Filename)

		dstPath := filepath.Join("upload", uniqueFilename)
		log.Println("Destination path for main image:", dstPath)
		dst, err := os.Create(dstPath)
		if err != nil {
			log.Println("Error creating destination file for main image:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error creating destination file for main image"})
		}
		defer dst.Close()

		// Save the main image file
		err = c.SaveFile(mainFile, dstPath)
		if err != nil {
			log.Println("Error saving main image file:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error saving main image file"})
		}

		mainImageURL = dstPath
	}

	// 处理颜色变体图片
	colorVariantFiles := form.File["color_variant_images"]
	for _, file := range colorVariantFiles {
		log.Println(file.Filename, file.Size, file.Header["Content-Type"][0])
		// 生成带时间戳的唯一文件名
		uniqueFilename := generateTimestampFilename(file.Filename)
		// Save the color variant image file
		dstPath := filepath.Join("upload", uniqueFilename)
		err := c.SaveFile(file, dstPath)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error saving color variant image file"})
		}

		colorVariantImages = append(colorVariantImages, models.Image{URL: dstPath, Type: "color_variant"})
	}

	// 处理介绍图片
	introductoryFiles := form.File["introductory_images"]

	for _, file := range introductoryFiles {
		log.Println(file.Filename, file.Size, file.Header["Content-Type"][0])
		// 生成带时间戳的唯一文件名
		uniqueFilename := generateTimestampFilename(file.Filename)
		// Save the introductory image file
		dstPath := filepath.Join("upload", uniqueFilename)
		err := c.SaveFile(file, dstPath)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error saving introductory image file"})
		}

		introductoryImages = append(introductoryImages, models.Image{URL: dstPath, Type: "introductory"})
	}

	product.Images = append([]models.Image{{URL: mainImageURL, Type: "main", MainImage: true}}, colorVariantImages...)
	product.Images = append(product.Images, introductoryImages...)

	// 插入产品到MongoDB
	insertResult, err := pc.collection.InsertOne(c.Context(), product)
	if err != nil {
		return c.JSON(fiber.Map{"error": "Failed to add product"})
	}

	// 返回成功响应，包括新创建的产品ID
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":   "Product added successfully",
		"productID": insertResult.InsertedID,
	})
}

func (pc *ProductController) DelProduct(c *fiber.Ctx) error {
	// 从路径参数中获取产品ID
	prodID := c.Params("id")

	// 验证ID格式
	if len(prodID) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Product ID is required"})
	}

	// 尝试从字符串转换ObjectID
	objectID, err := primitive.ObjectIDFromHex(prodID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid Product ID"})
	}

	// 定义删除过滤器
	filter := bson.M{"_id": objectID}

	// 使用DeleteOne方法根据ID删除产品
	deleteResult, err := pc.collection.DeleteOne(pc.ctx, filter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// 没有找到产品
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Product not found"})
		}
		// 删除出错
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	// 如果删除的文档数量为0，表示没有找到要删除的产品
	if deleteResult.DeletedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Product not found"})
	}

	// 返回成功的响应
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Product deleted successfully"})
}

func (pc *ProductController) UpdateProduct(c *fiber.Ctx) error {
	prodID := c.Params("id")
	objectID, err := primitive.ObjectIDFromHex(prodID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无效的产品ID"})
	}

	var updatedFields map[string]interface{}
	if err := c.BodyParser(&updatedFields); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "无法解析请求体"})
	}

	// 只更新提供的字段
	update := bson.M{"$set": updatedFields}

	result, err := pc.collection.UpdateOne(pc.ctx, bson.M{"_id": objectID}, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "更新产品时出错"})
	}

	if result.MatchedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到产品"})
	}
	// pc.ResetAllProductCategories(c)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "产品更新成功",
	})
}

// 重置所有产品的分类
func (pc *ProductController) ResetAllProductCategories(c *fiber.Ctx) error {
	// 第一步：删除所有产品的 Categories 字段
	_, err := pc.collection.UpdateMany(
		pc.ctx,
		bson.M{},
		bson.M{"$unset": bson.M{"categories": ""}},
	)
	if err != nil {
		log.Printf("删除 Categories 字段时出错: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "重置分类时出错"})
	}

	// 第二步：为所有产品添加一个空的 Categories 数组
	_, err = pc.collection.UpdateMany(
		pc.ctx,
		bson.M{},
		bson.M{"$set": bson.M{"categories": []models.CategoryRef{}}},
	)
	if err != nil {
		log.Printf("添加空 Categories 数组时出错: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "重置分类时出错"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "所有产品的分类已重置"})
}



func (pc *ProductController) AllProduct(c *fiber.Ctx) error {
	page, err := strconv.Atoi(c.Query("page", "1"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid page parameter"})
	}
	limit, err := strconv.Atoi(c.Query("limit", "10"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid limit parameter"})
	}

	skip := (page - 1) * limit
	findOptions := options.Find()
	findOptions.SetSkip(int64(skip))
	findOptions.SetLimit(int64(limit))

	cursor, err := pc.collection.Find(pc.ctx, bson.D{}, findOptions)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}
	defer cursor.Close(pc.ctx)
	var products []models.Product
	for cursor.Next(pc.ctx) {
		var product models.Product
		if err := cursor.Decode(&product); err != nil {
			log.Printf("解码产品时出错: %v", err)
			// 继续处理下一个文档，而不是立即返回错误
			continue
			// return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error decoding product"})
		}
		products = append(products, product)
	}

	if err := cursor.Err(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Cursor Error: " + err.Error()})
	}

	totalCount, err := pc.collection.CountDocuments(pc.ctx, bson.D{})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error counting products"})
	}

	response := struct {
		Products []models.Product `json:"products"`
		Total    int64            `json:"total"`
	}{
		Products: products,
		Total:    totalCount,
	}

	return c.Status(fiber.StatusOK).JSON(response)

}

// func (pc *ProductController) AllProduct(c *fiber.Ctx) error {
// 	// 创建一个空切片来存储查询结果
// 	var products []models.Product

// 	// 执行查询，获取所有产品
// 	cursor, err := pc.collection.Find(pc.ctx, bson.D{})
// 	if err != nil {
// 		// 如果查询出错，返回错误
// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
// 	}
// 	defer cursor.Close(pc.ctx)

// 	// 逐个解码文档到产品切片中
// 	for cursor.Next(pc.ctx) {
// 		var product models.Product
// 		if err := cursor.Decode(&product); err != nil {
// 			// 如果解码出错，返回错误
// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error decoding product"})
// 		}
// 		products = append(products, product)
// 	}

// 	if err := cursor.Err(); err != nil {
// 		// 检查游标是否有错误
// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Cursor Error: " + err.Error()})
// 	}
// 	// 将产品切片序列化为JSON并发送给客户端
// 	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"products": products})
// }

func (pc *ProductController) FetchOne(c *fiber.Ctx) error {
	// 从路径参数中获取产品ID
	prodID := c.Params("id")

	// 验证ID格式
	if len(prodID) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Product ID is required"})
	}

	// 尝试从字符串转换ObjectID
	objectID, err := primitive.ObjectIDFromHex(prodID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid Product ID"})
	}

	// 创建用于查询的结构体变量
	var product models.Product

	// 使用FindOne方法根据ID查询产品
	err = pc.collection.FindOne(pc.ctx, bson.M{"_id": objectID}).Decode(&product)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// 没有找到产品
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Product not found"})
		}
		// 查询出错
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	// 将查询到的产品信息序列化为JSON并返回
	return c.JSON(product)
}
