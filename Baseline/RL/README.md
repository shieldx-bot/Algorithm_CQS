# Baseline RL (Simple) — Epsilon-Greedy Contextual Bandit

Thư mục này chứa một baseline RL **tối giản và ổn định** để so sánh công bằng với các baseline khác (ví dụ: H_BAC).

Mục tiêu của baseline này là:

- Giữ **luồng request/feedback giống H_BAC** để bạn chỉ thay “thuật toán chọn backend”.
- Dễ chạy, dễ debug, ít tham số, ít rủi ro “nổ” khi load.

## 1) Đây là RL gì?

Đây là một dạng **contextual multi-armed bandit** (thường được xem như baseline RL đơn giản):

- **Context/State ($s$)**: mức tải tổng (global load) được *rời rạc hoá* từ `TotalOnQueue` gần nhất.
- **Action ($a$)**: chọn một backend (IP) để forward request.
- **Reward ($r$)**: đo “tốt/xấu” của quyết định. Ở đây dùng proxy tốc độ:

  $$r = \frac{1}{\text{TimeDoneTask} + \varepsilon}$$

  `TimeDoneTask` càng nhỏ thì reward càng lớn.
- **Policy**: epsilon-greedy trên bảng giá trị `Q[s][ip]`.

Vì là *bandit* nên update không dùng thành phần $\gamma \max_{a'} Q(s',a')$ như Q-learning đầy đủ; thay vào đó ta học “kỳ vọng reward” theo context.

## 2) Trạng thái (State) được định nghĩa thế nào?

State được bucket theo tỷ lệ:

- `ratio = totalQueue / RL_MAX_QUEUE`
- Bucket:
  - `0`: `ratio < 0.10` (low load)
  - `1`: `ratio < 0.35` (medium)
  - `2`: `ratio < 0.70` (high)
  - `3`: còn lại (overloaded)

Ý nghĩa: cùng một backend có thể “tốt” ở tải thấp nhưng “xấu” ở tải cao; `Q` tách theo state để mô hình hoá điều này.

## 3) Chọn backend (Policy: epsilon-greedy)

Tại thời điểm dispatch request:

- Với xác suất `RL_EPSILON`: **explore** → chọn backend ngẫu nhiên.
- Ngược lại: **exploit** → chọn backend có `Q[state][ip]` lớn nhất.

## 4) Cập nhật học (Update rule)

Khi nhận feedback từ `/receive-metrics`:

- Tính reward từ `TimeDoneTask`.
- Update Q theo EMA (exponential moving average):

  $$Q(s,a) \leftarrow Q(s,a) + \alpha \cdot (r - Q(s,a))$$

Trong đó `alpha = RL_ALPHA`.

### Gán feedback cho đúng “state lúc dispatch”

Baseline này lưu một FIFO per-IP (theo backend) để nhớ state tại lúc nó dispatch request sang backend đó.
Khi backend gửi metrics về (có `ip_vm`), balancer sẽ pop FIFO để lấy state tương ứng.

- Lý tưởng: **1 request → 1 feedback** và feedback về tương đối đúng thứ tự.
- Nếu bạn muốn đối sánh 1-1 tuyệt đối (không phụ thuộc thứ tự), có thể mở rộng thêm `request_id` (mình có thể patch giúp).

## 5) Chạy thử nghiệm (giống H_BAC)

### Run

```bash
cd Baseline/RL/Balancer
go mod tidy
go run .
```

### Cấu hình

- `RL_PORT` (default `8084`)
- `RL_EPSILON` (default `0.10`) — tăng để explore nhiều hơn
- `RL_ALPHA` (default `0.20`) — tăng để học nhanh hơn nhưng dễ nhiễu
- `RL_MAX_QUEUE` (default `1000`) — dùng cho bucket state
- `RL_VPS` danh sách backend, dạng `ip1,ip2,ip3`

Ví dụ chạy với danh sách backend:

```bash
RL_VPS="34.87.59.164,34.158.51.160" RL_PORT=8084 go run .
```

### Client gửi request vào balancer

```bash
curl -X POST http://localhost:8084/load-test-http3 \
  -H 'Content-Type: application/json' \
  -d '{"hello":"world"}' -i
```

Response headers có:

- `X-RL-Selected-IP`: backend được chọn
- `X-RL-State`: state bucket tại thời điểm chọn

### Backend/collector gửi feedback (metrics)

Ví dụ payload tối thiểu cần có `ip_vm`, `TimeDoneTask`, `total_on_queue`:

```bash
curl -X POST http://localhost:8084/receive-metrics \
  -H 'Content-Type: application/json' \
  -d '{
    "ip_vm":"34.87.59.164",
    "TimeDoneTask": 120,
    "TimeStartSend": 0,
    "Penumj": 1,
    "Pemips": 1,
    "NumberTask": 1,
    "ttj": 0,
    "tli": 1,
    "ifs": 0,
    "vmbw": 0,
    "total_on_queue": 10
  }'
```

## 6) Endpoints

- `GET /ping`
- `POST /load-test-http3` → forward tới `http://<ip>:8081/TestHTTP3`
- `POST /receive-metrics` → update `Q[state][ip]`
- `GET /debug/state` → xem Q-table + VPS stats hiện tại

## 7) Limitations (cố ý giữ đơn giản)

- Không dùng `s'`/trajectory → đây là bandit, không phải full RL (Q-learning/PPO).
- Reward chỉ dựa vào `TimeDoneTask` → nếu muốn cân bằng SLA/error/cost, bạn có thể thiết kế reward mới.
- FIFO mapping theo `ip_vm` phụ thuộc thứ tự feedback tương đối; muốn chắc chắn thì thêm `request_id`.
