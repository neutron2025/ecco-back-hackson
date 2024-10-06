package main

import (
	"blog-auth-server/controllers"
	"blog-auth-server/middleware"
	"blog-auth-server/utils"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/smartwalle/alipay/v3"
	"golang.org/x/net/context"
)

var ctx context.Context

// var err error
// var client *mongo.Client
var MongoUri string = "mongodb://neutronroot:pass123@127.0.0.1:27017/ecomm?authSource=admin"
var userController *controllers.UserController
var productController *controllers.ProductController
var cartController *controllers.CartController
var orderController *controllers.OrderController
var addressController *controllers.AddressController
var redemptionOrderController *controllers.RedemptionOrderController
var middleware1 *middleware.Middleware

func init() {

	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Printf("未能加载 .env 文件: %v", err)
	}

	// 创建上下文
	ctx = context.Background()

	// 连接 MongoDB
	utils.ConnectDB(MongoUri, ctx)
	db := utils.GetDB()
	usercollection := db.Collection("users")
	cartCollection := db.Collection("carts")
	productCollection := db.Collection("products")
	orderCollection := db.Collection("orders")
	addressCollection := db.Collection("address")
	statisticsCollection := db.Collection("order_cleanup_statistics")
	redemptionOrderCollection := db.Collection("redemption_orders")
	redisClient := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "password",
		DB:       0})
	status := redisClient.Ping(ctx)
	fmt.Println(status)

	//支付宝支付配置初始化
	var alipayClient *alipay.Client
	var err error
	// 初始化支付宝客户端
	// 请将 appId、privateKey 和 publicKey 替换为您的实际值
	alipayClient, err = alipay.New("2021004171687720", "alipay-private-key", true)
	if err != nil {
		log.Fatalf("Failed to initialize Alipay client: %v", err)
	}
	// 加载支付宝公钥
	err = alipayClient.LoadAliPayPublicKey("MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAhId8nwa+M1xfCFmRi+L227KJ0ezUb8LrtePsPOpESmIOgqxKh2F1BcQZ+ffoRV2Tzqf6Swat9RlUnGHWTDGfkXjijqoAGeX85g4Diii6KzoIy8kTU696r97o46X2OCip6LlilF1NI0MTPygDJYssG2+XP/Cin3YUc0psOcZu7XhcYHDVwcBPYJ/RakOBtjEui7scf3njCrF1srTGOGSdCYZCirs7LNVQiCzDNnGZGodb3jkBAcfTlWNhogKJB9hkr+DgIxpjt1zofnogD/whmnVm6do0XcIoPyyFMDgnBHP2du4gWQ6apUhsEU7nE7Tu9c0fTw8boJ5GBaIxMEY+BQIDAQAB")
	if err != nil {
		log.Fatalf("Failed to load Alipay public key: %v", err)
	}

	userController = controllers.NewUserController(usercollection, ctx, redisClient)
	productController = controllers.NewProductController(productCollection, ctx)

	cartController = controllers.NewCartController(cartCollection, productCollection, ctx)
	orderController = controllers.NewOrderController(usercollection, cartCollection, productCollection, orderCollection, addressCollection, statisticsCollection, ctx, alipayClient)
	addressController = controllers.NewAddressController(addressCollection, ctx)
	redemptionOrderController = controllers.NewRedemptionOrderController(redemptionOrderCollection, usercollection, orderCollection, ctx)

	middleware1 = middleware.NewMiddleware(ctx, redisClient)

}

func main() {
	// 创建安全中间件
	securityMiddleware := middleware.NewSecurityMiddleware()

	app := fiber.New()
	// 设置静态文件目录为 upload
	app.Static("/upload", "./upload")
	// 添加 CORS 中间件，允许所有源访问
	app.Use(cors.New(cors.Config{
		AllowMethods:     "GET,POST,PUT,DELETE",
		AllowHeaders:     "Content-Type,Authorization,Origin, Accept",
		AllowOrigins:     "https://shengchan.shop,https://huan270.cn,https://www.huan270.cn", // 允许前端应用的域名,生产环境要指定ip地址
		AllowCredentials: true,                                                               // 允许携带凭证

	}))

	app.Use(logger.New())

	api := app.Group("/api")
	api.Get("/", productController.AllProduct)          //产品展示页
	api.Get("/product/:id", productController.FetchOne) //产品信息页
	api.Post("/signup", userController.CreateUser)
	api.Post("/login", userController.Login)

	api.Get("/cart", middleware1.UserMiddlewareHandler, cartController.AllfromCart) //产品结算页 用户可以增删查 改数量,前端localStorage，登录后同步到数据库 在支付的时候需要登录session
	api.Post("/cart", middleware1.UserMiddlewareHandler, cartController.AddtoCart)  //后端接收到购物车数据后，将其与当前登录的用户账户关联起来, 关联成功后，前端可以清除localStorage
	api.Delete("/cart", middleware1.UserMiddlewareHandler, cartController.DelfromCart)
	api.Get("/userinfo", middleware1.UserMiddlewareHandler, userController.GetUserInfo)

	api.Post("/address", middleware1.UserMiddlewareHandler, addressController.AddAddress)                 //增
	api.Delete("/address/:addressID", middleware1.UserMiddlewareHandler, addressController.DelAddress)    //删
	api.Put("/address/:addressID", middleware1.UserMiddlewareHandler, addressController.UpdateAddress)    //改
	api.Get("/address", middleware1.UserMiddlewareHandler, addressController.GetAddress)                  //查
	api.Get("/address/:addressID", middleware1.UserMiddlewareHandler, addressController.FromIDGetAddress) //通过ID获取地址
	api.Post("/create_qr_code", middleware1.UserMiddlewareHandler, orderController.CreateQRCode)

	api.Post("/orders/create", middleware1.UserMiddlewareHandler, securityMiddleware.RateLimiter(), orderController.AddOrder)                                     //创建订单
	api.Get("/onepay", middleware1.UserMiddlewareHandler, orderController.GetOrder)                                             //查询个人所有订单
	api.Get("/query-auto", middleware1.UserMiddlewareHandler, securityMiddleware.RateLimiter(), orderController.QueryOrderAuto) //个人页面自动查询更新待支付订单，查询个人所有订单
	api.Get("/onepay/:orderID", middleware1.UserMiddlewareHandler, orderController.GetOneOrder)                                 //查询单个订单
	api.Post("/user/pow-addr", middleware1.UserMiddlewareHandler, userController.SetPowAddress)
	api.Get("/query_order/:orderID", middleware1.UserMiddlewareHandler, securityMiddleware.RateLimiter(), orderController.QueryOrder) //查询支付宝的支付信息,应用速率限制中间件
	api.Post("/transfer-scl", middleware1.UserMiddlewareHandler, securityMiddleware.RateLimiter(), orderController.TransferSCL)
	api.Post("/redemption-order", middleware1.UserMiddlewareHandler, securityMiddleware.RateLimiter(), redemptionOrderController.CreateRedemptionOrder)

	api.Get("/admininfo", middleware1.AdminMiddlewareHandler, userController.GetUserInfo)
	api.Get("/createadmin", userController.CreateAdminUser)
	api.Post("/adminTestRoute", middleware1.AdminMiddlewareHandler, userController.TestRoute)
	api.Get("/admin", middleware1.AdminMiddlewareHandler)                                                  //后台主页，展示销售数据,支付订单，未支付订单，数量和金钱，浏览数据统计
	api.Get("/admin/products", middleware1.AdminMiddlewareHandler, productController.AllProduct)           //展示后台产品数据
	api.Get("/admin/product/:id", middleware1.AdminMiddlewareHandler, productController.FetchOne)          //产品信息页
	api.Post("/admin/addproduct", middleware1.AdminMiddlewareHandler, productController.AddProduct)        //admin 添加产品
	api.Delete("/admin/delproduct/:id", middleware1.AdminMiddlewareHandler, productController.DelProduct)  //admin 删除产品
	api.Post("/admin/editproduct/:id", middleware1.AdminMiddlewareHandler, productController.UpdateProduct) //admin 编辑产品

	api.Get("/admin/users", middleware1.AdminMiddlewareHandler, userController.AllUsers)          //展示后台用户数据
	api.Get("/admin/user/:id", middleware1.AdminMiddlewareHandler, userController.GetOneUser)     //one user
	api.Delete("/admin/users/:id", middleware1.AdminMiddlewareHandler, userController.DelUser)    //admin删除用户
	api.Post("/admin/user/update-pow", middleware1.AdminMiddlewareHandler, userController.SetPow) //admin更新用户pow

	api.Get("/admin/orders", middleware1.AdminMiddlewareHandler, orderController.GetOrder)                     //展示后台 个人订单数据
	api.Get("/admin/orders/:orderID", middleware1.AdminMiddlewareHandler, orderController.GetOneOrderByID)     //展示后台 单个订单数据
	api.Get("/admin/address/:addressID", middleware1.AdminMiddlewareHandler, addressController.GetAddressByID) //展示后台 后台单个订单地址数据

	api.Get("/admin/redemption-orders", middleware1.AdminMiddlewareHandler, redemptionOrderController.GetRedemptionOrder)                               //展示后台 赎回订单数据
	api.Post("/admin/update-redemption-status/:dempOrderID", middleware1.AdminMiddlewareHandler, redemptionOrderController.UpdateRedemptionOrderStatus) //更新赎回订单状态
	api.Post("/admin/delredemption-orders/:dempOrderID", middleware1.AdminMiddlewareHandler, redemptionOrderController.DeleteRedemptionOrder)           //删除赎回订单


	api.Get("/admin/sales", middleware1.AdminMiddlewareHandler, orderController.GetSales)       //展示后台销售数据
	api.Get("/admin/visitors", middleware1.AdminMiddlewareHandler, orderController.GetVisitors) //展示后台浏览数据

	api.Get("/admin/analytics/sales", middleware1.AdminMiddlewareHandler, orderController.GetSalesAnalytics)       //展示后台销售数据分析
	api.Get("/admin/analytics/visitors", middleware1.AdminMiddlewareHandler, orderController.GetVisitorsAnalytics) //展示后台浏览数据分析

	api.Get("/admin/export_orders", middleware1.AdminMiddlewareHandler, orderController.ExportOrders) //后台导出订单到excel
	api.Get("/admin/allorders", middleware1.AdminMiddlewareHandler, orderController.GetAllOrders)     //查询所有订单
	// api.Get("/admin/paidorders", middleware1.AdminMiddlewareHandler, orderController.GetAllPaidOrder) //查询所有已支付订单

	// api.Post("/admin/clear-unpaid-orders", middleware1.AdminMiddlewareHandler, orderController.ClearUnpaidOrders) //清除未支付订单

	api.Post("/admin/update-ship-status", middleware1.AdminMiddlewareHandler, orderController.UpdateOrderItemShippingStatus) //更新订单发货状态
	api.Post("/admin/update-deliver-id", middleware1.AdminMiddlewareHandler, orderController.UpdateOrderItemDeliverID)       //更新订单快递单号
	api.Get("/admin/get-deliver-id", middleware1.AdminMiddlewareHandler, orderController.GetOrderItemDeliverID)              //获取订单快递单号

	err := app.Listen(":3000")
	if err != nil {
		log.Fatal("Error in running the server")
		return
	}
	log.Println("Server is running")
}

//nvwacms
//post
