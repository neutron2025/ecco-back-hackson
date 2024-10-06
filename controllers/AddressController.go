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

type AddressController struct {
	addressCollection *mongo.Collection
	ctx               context.Context
}

// NewCartController 构造函数
func NewAddressController(addressCollection *mongo.Collection, ctx context.Context) *AddressController {
	return &AddressController{
		addressCollection: addressCollection,
		ctx:               ctx,
	}
}

// AddAddress 添加新地址
func (ac *AddressController) AddAddress(c *fiber.Ctx) error {
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

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var newAddressItem models.AddressItem
	if err := c.BodyParser(&newAddressItem); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address data"})
	}
	newAddressItem.ID = primitive.NewObjectID()

	// 检查用户是否已有地址记录
	var existingAddress models.Address
	err = ac.addressCollection.FindOne(ac.ctx, bson.M{"user_ref": userID}).Decode(&existingAddress)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// 用户没有地址记录，创建新的
			newAddress := models.Address{
				ID:             primitive.NewObjectID(),
				UserRef:        userID,
				AddressDetails: []models.AddressItem{newAddressItem},
			}
			_, err = ac.addressCollection.InsertOne(ac.ctx, newAddress)
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to check existing address"})
		}
	} else {
		// 用户已有地址记录，添加新地址项
		_, err = ac.addressCollection.UpdateOne(
			ac.ctx,
			bson.M{"user_ref": userID},
			bson.M{"$push": bson.M{"address_detail": newAddressItem}},
		)
	}

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to add address"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Address added successfully", "address_id": newAddressItem.ID})
}

// GetAddress 获取用户的所有地址
func (ac *AddressController) GetAddress(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var address models.Address
	err = ac.addressCollection.FindOne(ac.ctx, bson.M{"user_ref": userID}).Decode(&address)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"message": "No addresses found for this user"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve addresses"})
	}

	return c.Status(fiber.StatusOK).JSON(address.AddressDetails)
}

// DelAddress 删除指定的地址
func (ac *AddressController) DelAddress(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	addressIDStr := c.Params("addressID")
	addressID, err := primitive.ObjectIDFromHex(addressIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address ID"})
	}

	result, err := ac.addressCollection.UpdateOne(
		ac.ctx,
		bson.M{"user_ref": userID},
		bson.M{"$pull": bson.M{"address_detail": bson.M{"_id": addressID}}},
	)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete address"})
	}

	if result.ModifiedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Address not found"})
	}

	return c.JSON(fiber.Map{"message": "Address deleted successfully"})
}

// UpdateAddress 更新指定的地址
func (ac *AddressController) UpdateAddress(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	addressIDStr := c.Params("addressID")
	addressID, err := primitive.ObjectIDFromHex(addressIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address ID"})
	}

	var updatedAddress models.AddressItem
	if err := c.BodyParser(&updatedAddress); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address data"})
	}

	updatedAddress.ID = addressID

	result, err := ac.addressCollection.UpdateOne(
		ac.ctx,
		bson.M{
			"user_ref":           userID,
			"address_detail._id": addressID,
		},
		bson.M{"$set": bson.M{"address_detail.$": updatedAddress}},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update address"})
	}

	if result.ModifiedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Address not found"})
	}

	return c.JSON(fiber.Map{"message": "Address updated successfully"})
}

// 用户通过ID获取地址
func (ac *AddressController) FromIDGetAddress(c *fiber.Ctx) error {
	claims, ok := c.Locals("claims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error"})
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "User ID not found in claims"})
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	addressIDStr := c.Params("addressID")
	addressID, err := primitive.ObjectIDFromHex(addressIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address ID"})
	}

	var address models.Address
	err = ac.addressCollection.FindOne(ac.ctx, bson.M{"user_ref": userID, "address_detail._id": addressID}).Decode(&address)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Address not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve address"})
	}
	// 在地址列表中查找匹配的地址项
	var targetAddress models.AddressItem
	for _, item := range address.AddressDetails {
		if item.ID == addressID {
			targetAddress = item
			break
		}
	}

	if targetAddress.ID.IsZero() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到地址"})
	}

	return c.JSON(targetAddress)

}

// 管理员根据地址ID获取地址
// 管理员根据地址ID获取地址
func (ac *AddressController) GetAddressByID(c *fiber.Ctx) error {
	addressIDStr := c.Params("addressID")
	log.Printf("Attempting to get address with ID: %s", addressIDStr)

	addressID, err := primitive.ObjectIDFromHex(addressIDStr)
	if err != nil {
		log.Printf("Invalid address ID: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid address ID"})
	}

	var address models.Address
	err = ac.addressCollection.FindOne(ac.ctx, bson.M{"address_detail._id": addressID}).Decode(&address)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			log.Printf("Address not found for ID: %s", addressIDStr)
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Address not found"})
		}
		log.Printf("Error retrieving address: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve address"})
	}

	// 在地址列表中查找匹配的地址项
	var targetAddress models.AddressItem
	for _, item := range address.AddressDetails {
		if item.ID == addressID {
			targetAddress = item
			break
		}
	}

	if targetAddress.ID.IsZero() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未找到地址"})
	}

	log.Printf("Successfully retrieved address for ID: %s", addressIDStr)
	return c.JSON(targetAddress)
}
