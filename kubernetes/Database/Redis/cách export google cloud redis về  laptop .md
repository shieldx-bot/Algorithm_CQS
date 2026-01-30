OK — trạng thái của bạn hiện giờ **không hỏng**, chỉ là bị **đụng port-forward 6379** và thiếu `redis-cli`. Tôi tóm tắt đúng tình huống kỹ thuật:

---

# 🎯 Những gì đang xảy ra

## ✅ Bạn đã mở SSH tunnel thành công

```
gcloud compute ssh ... -L 6379:localhost:6379
```

→ Port **6379 trên VM** đã bị chiếm bởi SSH tunnel.

---

## ❌ Vì vậy kubectl port-forward báo lỗi

```
bind: address already in use
```

Vì:

```
kubectl port-forward svc/redis 6379:6379
```

cũng muốn chiếm port 6379 trên VM → xung đột.

---

# ✅ Cách sửa đúng (đổi port phía VM)

Chạy port-forward Redis trên **port khác** ở VM, ví dụ 6380:

Trong cửa sổ SSH vào VM:

```bash
kubectl -n postgre-db port-forward svc/redis 6380:6379
```

---

# ✅ Sau đó sửa tunnel phía laptop

Thoát SSH tunnel hiện tại (Ctrl+C), rồi mở lại:

```bash
gcloud compute ssh node5 --zone asia-northeast1-b \
  -- -L 6379:localhost:6380
```

## 🧠 Ý nghĩa

```
Laptop :6379 → VM :6380 → Redis :6379
```

Không còn đụng port.

---

# ✅ Cài redis-cli (bạn đang gõ trong VM nên nó báo thiếu)

Nếu bạn muốn test ngay **trên VM**:

```bash
sudo apt update
sudo apt install redis-tools -y
```

Test:

```bash
redis-cli -h 127.0.0.1 -p 6380
PING
```

---

# ✅ Test từ laptop (khuyến nghị)

Cài redis-cli trên laptop:

```bash
sudo apt install redis-tools
```

Sau khi tunnel mở:

```bash
redis-cli -h 127.0.0.1 -p 6379
PING
```

---

# 🧪 Kiểm tra port nào đang chiếm 6379 trên VM

Nếu muốn xem rõ:

```bash
sudo ss -ltnp | grep 6379
```

Bạn sẽ thấy process ssh đang giữ port.

---

# 📌 Quy tắc chuẩn khi làm tunnel + port-forward

Luôn tránh dùng cùng port:

```
kubectl port-forward : A
ssh -L : B
```

A ≠ B

---

Nếu bạn muốn, tôi có thể đưa cho bạn một sơ đồ chuẩn:

* Redis trong K3s
* App pod truy cập nội bộ
* Laptop truy cập debug
* Không cần port-forward thủ công mỗi lần.
