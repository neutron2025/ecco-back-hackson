# Application Websites

- Inside China: [https://huan270.cn/](https://huan270.cn/)
- Outside China: [https://shengchan.shop/](https://shengchan.shop/)


# White Paper

- whitepaper [https://shengchan.link/](https://shengchan.link/)




## 生产环境下的nginx反代===========
```
server {
    listen 80;
    server_name shengchan.shop;

    location / {
        proxy_pass http://118.193.43.192:5000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
    }

    location /api/ {
        proxy_pass http://118.193.43.192:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;

        # 添加 CORS 相关的头信息
        add_header 'Access-Control-Allow-Origin' 'http://118.193.43.192';
        add_header 'Access-Control-Allow-Methods' 'GET, POST, OPTIONS, PUT, DELETE';
        add_header 'Access-Control-Allow-Headers' 'DNT,X-Mx-ReqToken,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type';

        # 安全头
        add_header X-Frame-Options "SAMEORIGIN";
        add_header X-XSS-Protection "1; mode=block";
        add_header X-Content-Type-Options "nosniff";

        if ($request_method = 'OPTIONS') {
            return 204;
        }
    }
}
```

## 前端编译后部署到nginx 的==  将build 里面的文件复制到/var/www/shengchan.shop 文件夹里,还有图片路径也要nginx 指定
```
server {
    listen 80;
    server_name shengchan.shop;

    root /var/www/shengchan.shop;  # 指向您的构建文件目录
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;  # 对于 SPA 的路由处理
    }

    location /upload {
        alias /home/ubuntu/ecco-back/upload;  # 图片 替换为实际路径
        try_files $uri $uri/ =404;
        # 限制文件类型
        location ~* \.(jpg|jpeg|png|gif|ico|css|js)$ {
        expires 30d;
        add_header Cache-Control "public, no-transform";
        }
    }


    location /api/ {
        proxy_pass http://118.193.43.192:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;

        # 添加 CORS 相关的头信息
        add_header 'Access-Control-Allow-Origin' 'http://118.193.43.192';
        add_header 'Access-Control-Allow-Methods' 'GET, POST, OPTIONS, PUT, DELETE';
        add_header 'Access-Control-Allow-Headers' 'DNT,X-Mx-ReqToken,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type';

        # 安全头
        add_header X-Frame-Options "SAMEORIGIN";
        add_header X-XSS-Protection "1; mode=block";
        add_header X-Content-Type-Options "nosniff";

        if ($request_method = 'OPTIONS') {
            return 204;
        }
    }
    # 添加缓存控制，避免浏览器缓存旧的静态资源
    location ~* \.(js|css|png|jpg|jpeg|gif|ico)$ {
        expires max;
        add_header Cache-Control public;
    }
}
```

```
sudo nginx -t
sudo systemctl reload nginx

sudo systemctl restart nginx

确保 Nginx 进程有权限读取这些文件 ps aux | grep nginx  查看nginx用户名  设置完后重启nginx
   sudo chown -R www-data:www-data /home/ubuntu/ecco-back/upload
   sudo chmod -R 755 /home/ubuntu/ecco-back/upload
```

## 故障排除
```
当首页无法查看到图片的时候
1、确认文件存在     
    ls -l /home/ubuntu/ecco-back/upload/1\ \(14\).jpg

2、检查 Nginx 配置  sudo nano /etc/nginx/sites-available/default 
确保有类似这样的配置：
   location /upload {
       alias /home/ubuntu/ecco-back/upload;
       try_files $uri $uri/ =404;
   }
3. 检查 Nginx 错误日志：   sudo tail -f /var/log/nginx/error.log
4、文件名中包含空格和括号，可能需要在 URL 中正确编码。在前端代码中，使用 encodeURIComponent() 函数来编码文件名：
    const fileName = encodeURIComponent("1 (14).jpg");
    const imageUrl = `http://shengchan.shop/upload/${fileName}`;

5、检查文件所有权和权限    ls -l /home/ubuntu/ecco-back/upload/
   检查上传目录及其父目录的权限：

    ls -ld /home/ubuntu/ecco-back/upload
    ls -ld /home/ubuntu/ecco-back
    ls -ld /home/ubuntu
 
    sudo chmod 775 /home/ubuntu/ecco-back/upload
    sudo chmod 775 /home/ubuntu/ecco-back
   确保父目录可访问

    sudo chmod 755 /home/ubuntu/ecco-back
    sudo chmod 755 /home/ubuntu
```
## nginx 实现ssl  详见certbot
## 图片上传故障  
```
对于 Nginx，可以在 nginx.conf 文件中调整 client_max_body_size 参数来增加允许的最大请求实体大小。例如，设置为 client_max_body_size 20M; 表示允许最大 20MB 的请求实体
```
## 运行程序 nohup go run . > output.log 2>&1 &