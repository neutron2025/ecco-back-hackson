package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID          primitive.ObjectID `json:"id" bson:"_id"`
	Email       string             `json:"email" bson:"email"`       // 用户邮箱，可以登录
	Password    string             `json:"password" bson:"password"` // 用户电话，可以登录
	Phone       string             `json:"phone" bson:"phone"`
	Pow         float64            `json:"pow" bson:"pow"`         //权证数量
	PowAddress  string             `json:"powaddr" bson:"powaddr"` //权证地址
	Permissions Permissions        `json:"permissions" bson:"permissions"`
	CreatedAt   time.Time          `json:"created_at"`
	Updated_At  time.Time          `json:"updated_at"`
}

type Address struct {
	ID             primitive.ObjectID `bson:"_id"`
	UserRef        primitive.ObjectID `bson:"user_ref"` // 关联的用户ID
	AddressDetails []AddressItem      `json:"address_detail" bson:"address_detail"`
}

type AddressItem struct {
	ID        primitive.ObjectID `bson:"_id"`
	Phone     string             `json:"phone" bson:"phone"`
	FirstName string             `json:"first_name"`
	LastName  string             `json:"last_name"`
	Street    string             `json:"street"`
	City      string             `json:"city"`
	State     string             `json:"state"`
	ZipCode   string             `json:"zip_code"`
	IsDefault bool               `json:"is_default"`
}

type Orders struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserRef            primitive.ObjectID `bson:"user_ref" json:"user_ref"`               // 关联的用户ID
	AlipayTradeNo      string             `bson:"alipay_trade_no" json:"alipay_trade_no"` // 支付宝交易号
	OrderItems         []OrderItem        `bson:"items" json:"items"`
	TotalPrice         uint64             `bson:"total_price" json:"total_price"`
	Discount           int                `bson:"discount" json:"discount"`
	PaymentStatus      string             `bson:"payment_status" json:"payment_status"` // 支付状态
	PaymentTime        time.Time          `bson:"payment_time" json:"payment_time"`     // 支付时间
	BuyerAlipayAccount string             `bson:"buyer_alipay_account" json:"buyer_alipay_account"`
	CreatedAt          time.Time          `bson:"created_at" json:"created_at"`
	IsRedeemed         bool               `bson:"is_redeemed" json:"is_redeemed"`
}

type OrderItem struct {
	ProductRef     primitive.ObjectID `bson:"product_ref" json:"product_ref"` // 关联的产品ID
	Quantity       int                `bson:"quantity" json:"quantity"`
	Size           string             `bson:"size" json:"size"`
	Color          string             `bson:"color" json:"color"`
	Price          uint64             `bson:"price" json:"price"`
	DeliverID      string             `bson:"deliver_id" json:"deliverid"`              // 快递单号
	ShippingStatus string             `bson:"shipping_status" json:"shipping_status"`   // 配送状态
	AddressItemRef primitive.ObjectID `bson:"address_item_ref" json:"address_item_ref"` // 每个商品的配送地址
}

type Product struct {
	ID          primitive.ObjectID `bson:"_id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	SizeColors  []SizeColor        `json:"size_colors"` // 存储尺寸和颜色的对应关系
	Price       uint64             `json:"price"`
	CreatedAt   time.Time          `json:"created_at"`
	Rating      float64            `json:"rating"`                       // 平均评分
	Images      []Image            `json:"images"`                       // 图片URL数组
	Categories  []CategoryRef      `json:"categories" bson:"categories"` // 产品分类引用列表
	Inventory   int                `json:"inventory"`                    // 库存数量
}

type Image struct {
	URL       string `json:"url"`        // 图片的URL
	Type      string `json:"type"`       // 图片类型，例如："main", "color_variant", "introductory"
	Color     string `json:"color"`      // 如果图片是颜色变体，这里存储颜色信息
	MainImage bool   `json:"main_image"` // 标记这张图片是否为主视图
}

type SizeColor struct {
	Size   string   `json:"size"`
	Colors []string `json:"colors"`
}

type RedemptionOrder struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserRef        primitive.ObjectID `bson:"user_ref" json:"user_ref"`         // 关联的用户ID
	OrderRef       primitive.ObjectID `bson:"order_ref" json:"order_ref"`       // 关联的原始订单ID
	IsSubmitted    bool               `bson:"is_submitted" json:"is_submitted"` // 是否已提交
	Status         string             `bson:"status" json:"status"`             // 赎回状态，如：待处理、已完成、已取消
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	AlipayUsername string             `bson:"alipay_username" json:"alipay_username"` // 支付宝用户名
	AlipayAccount  string             `bson:"alipay_account" json:"alipay_account"`   // 支付宝账户
	WalletAddress  string             `bson:"wallet_address" json:"wallet_address"`   // 钱包地址
	Hash           string             `bson:"hash" json:"hash"`                       // 哈希
}

type Category struct {
	ID          primitive.ObjectID `bson:"_id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Products    []ProductRef       `json:"products" bson:"products"` // 分类下的产品引用列表
}

type Payment struct {
	OrderRef primitive.ObjectID `bson:"order_ref"` // 关联的订单ID
	Method   string             `json:"method"`    // 支付方式，如信用卡、PayPal、COD等
	Status   string             `json:"status"`    // 支付状态，如成功、失败等
	Amount   uint64             `json:"amount"`
}

type Permissions struct {
	AdminFlag bool `json:"admin_flag" bson:"admin_flag"`
}

type Cart struct {
	ID        primitive.ObjectID `bson:"_id"`
	UserRef   primitive.ObjectID `bson:"user_ref"` // 关联的用户ID
	CartItems []CartItem         `bson:"items"`
}

type CartItem struct {
	ProductRef primitive.ObjectID `bson:"product_ref"` // 关联的产品ID
	Quantity   int                `bson:"quantity"`
	Size       string             `bson:"size"`
	Color      string             `bson:"color"`
	// 可以添加其他字段，比如尺寸、颜色等，如果产品有这些属性的话
}

type OrderCleanupStatistics struct {
	ID               primitive.ObjectID `bson:"_id,omitempty"`
	CleanupDate      time.Time          `bson:"cleanup_date"`
	DeletedCount     int64              `bson:"deleted_count"`
	TotalAmountSaved float64            `bson:"total_amount_saved"`
}

// type Review struct {
// 	ID         primitive.ObjectID `bson:"_id"`
// 	ProductRef primitive.ObjectID `bson:"product_ref"` // 关联的产品ID
// 	UserRef    primitive.ObjectID `bson:"user_ref"`    // 关联的用户ID
// 	Comment    string             `json:"comment"`
// 	Rating     int                `json:"rating"`
// 	CreatedAt  time.Time          `json:"created_at"`
// }

// 这些模型是用于简化查询结果的引用模型
type CategoryRef struct {
	ID   primitive.ObjectID `bson:"_id"`
	Name string             `json:"name"`
}

type ProductRef struct {
	ID    primitive.ObjectID `bson:"_id"`
	Name  string             `json:"name"`
	Price uint64             `json:"price"`
}
type OrderRef struct {
	ID        primitive.ObjectID `bson:"_id"`
	OrderDate time.Time          `json:"order_date"`
	Status    string             `json:"status"`
}

type ProductUser struct {
	ProductRef  primitive.ObjectID `bson:"product_ref" json:"product_ref"` // 引用Product的ID
	Quantity    int                `bson:"quantity" json:"quantity"`       // 用户购买的数量
	IsPurchased bool               `json:"is_purchased" bson:"is_purchased"`
}
