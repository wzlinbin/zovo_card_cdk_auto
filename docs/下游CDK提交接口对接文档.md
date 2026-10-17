# 下游 CDK 接口对接说明

本站地址：

```text
http://cdkey.ocisg.xyz
```

下游每次处理一张 CDK，按下面两步调用。同一张 CDK 的两次请求必须使用相同的 `code`。

## 第一步：提交 CDK，验证 CDK

请求地址：

```text
POST http://cdkey.ocisg.xyz/api/v1/public/cdk/submit
```

请求头：

```http
Content-Type: application/json
```

请求内容：

```json
{
  "code": "真实CDK-001"
}
```

### cURL 示例

```bash
curl -X POST "http://cdkey.ocisg.xyz/api/v1/public/cdk/submit" \
  -H "Content-Type: application/json" \
  -d '{"code":"真实CDK-001"}'
```

本站会返回 CDK 验证结果。验证失败时，直接把返回的提示展示给用户；验证成功后，再让用户输入 Session。

## 第二步：提交 CDK 和 Session，开始兑换

请求地址不变：

```text
POST http://cdkey.ocisg.xyz/api/v1/public/cdk/submit
```

请求内容：

```json
{
  "code": "真实CDK-001",
  "session": "用户输入的Session"
}
```

### cURL 示例

```bash
curl -X POST "http://cdkey.ocisg.xyz/api/v1/public/cdk/submit" \
  -H "Content-Type: application/json" \
  -d '{"code":"真实CDK-001","session":"用户输入的Session"}'
```

本站会自动完成预检和兑换。下游不需要提交 `request_id`、`batch_id`、`redemption_token`、`preflight_token` 或 `credential`。

第二步返回的是“兑换请求已受理”，不一定已经完成。例如返回 `queued`、`awaiting_card`、`running`、`pending` 时，说明仍在处理，不能直接提示用户充值成功。

## 查询最终结果

请求地址：

```text
GET http://cdkey.ocisg.xyz/api/v1/public/cdk/result-by-code?code=真实CDK-001
```

cURL 示例：

```bash
curl "http://cdkey.ocisg.xyz/api/v1/public/cdk/result-by-code?code=%E7%9C%9F%E5%AE%9ECDK-001"
```

建议每 3～5 秒查询一次，直到返回状态为：

```text
completed = 兑换成功
```

如果返回 `declined`、`failed_precharge`、`cancelled` 等状态，表示兑换失败，具体原因以接口返回内容为准。

## JavaScript 示例

```javascript
const API = "http://cdkey.ocisg.xyz";

async function submitCDK(code, session) {
  const body = session ? { code, session } : { code };
  const response = await fetch(`${API}/api/v1/public/cdk/submit`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify(body)
  });

  return {
    httpStatus: response.status,
    data: await response.json()
  };
}

// 第一步：验证 CDK
const check = await submitCDK("真实CDK-001");

// 用户输入 Session 后，执行第二步
const redeem = await submitCDK("真实CDK-001", "用户输入的Session");

// 查询最终结果
const result = await fetch(
  `${API}/api/v1/public/cdk/result-by-code?code=${encodeURIComponent("真实CDK-001")}`
).then((response) => response.json());
```

## 注意事项

1. 一张 CDK 对应一个 Session，一次请求只提交一张 CDK。
2. 多张 CDK 请由下游自己循环调用，一张一张处理，不要提交数组。
3. 第二步如果网络超时，不要立刻重复提交，先查询最终结果。
4. 当前接口不需要 API Key，也不需要下游生成 `request_id` 或 `batch_id`。
5. CDK 和 Session 不要写入日志、URL 或浏览器本地存储。
