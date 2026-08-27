import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import { mailApi, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";

interface ExportDialogProps {
  tenantID: TenantRef;
  // 当前筛选中的分组与已选中的账号，决定可选的导出范围。
  groupID: string | null;
  selectedIDs: string[];
  onClose: () => void;
}

type Scope = "selected" | "group" | "all";

// ExportDialog 是凭据的唯一出口，因此界面上要把两件事说清楚：
// 导出的是明文，以及这次操作会被记进审计。含糊其辞不会让它更安全，
// 只会让用户不知道自己刚做了什么。
export function ExportDialog({ tenantID, groupID, selectedIDs, onClose }: ExportDialogProps) {
  const [scope, setScope] = useState<Scope>(selectedIDs.length > 0 ? "selected" : "all");
  const { error, pending, run } = useAsyncAction();

  const options = [
    ...(selectedIDs.length > 0
      ? [{ label: `已选中的 ${selectedIDs.length} 个账号`, value: "selected" }]
      : []),
    ...(groupID ? [{ label: "当前分组", value: "group" }] : []),
    { label: "全部账号", value: "all" },
  ];

  function submit(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      const content = await mailApi.exportAccounts(tenantID, {
        scope,
        account_ids: scope === "selected" ? selectedIDs : [],
        group_ids: scope === "group" && groupID ? [groupID] : [],
      });
      // 没有凭据的账号会被后端跳过，整批都没凭据时导出是空的。
      // 这时候不能照样下载一个空文件——用户会以为导出成功了，
      // 直到过几天拿这个文件去恢复才发现里面什么都没有。
      // 条数从内容里数，不读 X-Export-Count：前后端分域部署时
      // 没配 Access-Control-Expose-Headers，那个头在浏览器里读不到。
      if (content.trim() === "") {
        throw new Error("这批账号里没有可导出的凭据（密码与刷新令牌都为空）");
      }
      download(content);
      onClose();
    });
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard render={<form onSubmit={submit} />} className="w-full max-w-md p-5">
        <h2 className="text-lg font-semibold text-kumo-strong">导出账号</h2>
        <p className="mt-1 text-sm text-kumo-subtle">
          导出文件包含密码与刷新令牌的<strong className="text-kumo-danger">明文</strong>
          ，可被本平台重新导入。本次操作会记入审计日志。
        </p>

        <div className="mt-4 flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            导出范围
            <Select
              aria-label="导出范围"
              items={options}
              value={scope}
              onValueChange={(value: string | null) => setScope((value ?? "all") as Scope)}
            />
          </label>
        </div>

        {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" variant="primary" disabled={pending}>
            {pending ? "导出中…" : "导出"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}

// download 把文本存成本地文件。用 a[download] 而不是新开窗口：
// 后者会把凭据明文直接铺在浏览器标签页里，还会留在历史记录中。
function download(content: string) {
  const url = URL.createObjectURL(new Blob([content], { type: "text/plain;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = `accounts-${new Date().toISOString().slice(0, 10)}.txt`;
  link.click();
  URL.revokeObjectURL(url);
}
