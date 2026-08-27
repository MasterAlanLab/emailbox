import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import { mailApi, type GroupColor, type MailGroupNode, type TenantRef } from "@/api/mail";
import { GROUP_COLORS } from "@/lib/groupColor";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { GroupDot } from "./GroupDot";

interface GroupFormDialogProps {
  tenantID: TenantRef;
  /** 传了就是编辑，不传就是新建。 */
  group?: MailGroupNode;
  onClose: () => void;
  onSaved: () => void;
}

export function GroupFormDialog({ tenantID, group, onClose, onSaved }: GroupFormDialogProps) {
  const editing = group !== undefined;
  const [name, setName] = useState(group?.name ?? "");
  const [description, setDescription] = useState(group?.description ?? "");
  const [color, setColor] = useState<GroupColor>(group?.color ?? "gray");
  const { error, pending, run } = useAsyncAction();

  function submit(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      if (editing) {
        await mailApi.updateGroup(tenantID, group.id, { name, description, color });
      } else {
        await mailApi.createGroup(tenantID, { name, description, color });
      }
      onSaved();
    });
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard render={<form onSubmit={submit} />} className="w-full max-w-md overflow-auto p-5">
        <h2 className="text-lg font-semibold text-kumo-strong">
          {editing ? "编辑分组" : "新建分组"}
        </h2>

        <div className="mt-4 flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            名称
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
              autoFocus
              required
            />
          </label>

          <label className="flex flex-col gap-1 text-sm">
            描述（可选）
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="例如：这批账号只用于 A 客户的验证码接收"
            />
          </label>

          {/* 颜色是分组唯一的视觉标识，选完直接把那个圆点摆在旁边，
              省得用户存了才知道选中的是哪一个。 */}
          <label className="flex flex-col gap-1 text-sm">
            颜色
            <div className="flex items-center gap-2">
              <GroupDot color={color} />
              <Select
                className="flex-1"
                aria-label="颜色"
                value={color}
                onValueChange={(v: string | null) => setColor((v ?? "gray") as GroupColor)}
              >
                {GROUP_COLORS.map((c) => (
                  <Select.Option key={c} value={c}>
                    {COLOR_LABEL[c]}
                  </Select.Option>
                ))}
              </Select>
            </div>
          </label>
        </div>

        {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" variant="primary" disabled={pending || !name.trim()}>
            {pending ? "保存中…" : editing ? "保存" : "创建"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}

const COLOR_LABEL: Record<GroupColor, string> = {
  blue: "蓝",
  green: "绿",
  amber: "琥珀",
  red: "红",
  purple: "紫",
  gray: "灰",
};
