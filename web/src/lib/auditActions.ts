// 审计动作的中文名，与后端 pkg/model/audit.go 的 Audit* 常量一一对应。
// pkg/model/audit_test.go 会检查每个后端常量在这里都有名字——漏一条，页面上就会冒出英文代码。
//
// 有几个动作是合并记录的，名字按实际含义起：account.batch 覆盖批量移动 / 改状态 / 改代理，
// job.submit 覆盖令牌刷新与账号检测，billing.cancel 覆盖取消与恢复自动续费。
export const AUDIT_ACTION_LABEL: Record<string, string> = {
  "account.list": "查看账号列表",
  "account.read": "查看账号详情",
  "account.create": "添加邮箱账号",
  "account.update": "修改邮箱账号",
  "account.delete": "删除邮箱账号",
  "account.import": "批量导入账号",
  "account.batch": "批量修改账号",
  "account.export": "导出账号凭据",
  "message.read": "查看邮件",
  "message.write": "标记或删除邮件",
  "group.write": "修改分组",
  "group.proxy_reveal": "查看分组代理明文",
  "api_key.reset": "重置 API Key",
  "token.refresh": "刷新令牌",
  "token.reauthorize": "重新授权邮箱",
  "job.submit": "提交批量任务",
  "job.stop": "停止批量任务",
  "user.update": "修改用户",
  "user.delete": "删除用户",
  "user.reset_password": "重置用户密码",
  "plan.create": "新建套餐",
  "plan.update": "修改套餐",
  "plan.delete": "删除套餐",
  "plan.assign": "为用户分配套餐",
  "billing.settings.update": "开关在线订阅",
  "billing.price.update": "修改套餐价格",
  "billing.checkout": "发起订阅付款",
  "billing.cancel": "取消或恢复自动续费",
  // 已废弃的动作：旧记录仍然存在，名字要留着。
  "quota.update": "调整配额（旧）",
};

export const auditActionLabel = (action: string) => AUDIT_ACTION_LABEL[action] ?? action;

export const ACTOR_KIND_LABEL: Record<string, string> = {
  admin: "管理员",
  user: "用户",
  api_key: "API Key",
  system: "系统",
};
