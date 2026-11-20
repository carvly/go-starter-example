import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/errors";

import { ChangePasswordPage } from "./change-password-page";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mutateAsync = vi.fn();
vi.mock("./queries", () => ({
  useChangePassword: () => ({ mutateAsync }),
}));
vi.mock("@/lib/session", () => ({
  useMe: () => ({ must_change_password: true }),
  useLogout: () => ({ mutate: vi.fn() }),
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));

function type(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

async function submitWrongCurrentPassword() {
  mutateAsync.mockRejectedValueOnce(new ApiError(400, "wrong_password", "wrong password"));
  const el = document.createElement("div");
  document.body.append(el);
  const root = createRoot(el);
  act(() => root.render(<ChangePasswordPage />));
  const input = (name: string) => el.querySelector<HTMLInputElement>(`input[name="${name}"]`)!;
  act(() => {
    type(input("current_password"), "typo-temp-password");
    type(input("new_password"), "NewPassw0rd!!");
    type(input("confirm_password"), "NewPassw0rd!!");
  });
  await act(async () => el.querySelector("form")!.requestSubmit());
  return { el, current: input("current_password"), unmount: () => act(() => root.unmount()) };
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("ChangePasswordPage", () => {
  it("临时密码不正确时，错误显示在临时密码字段下，而不是表单顶部", async () => {
    const { el, current, unmount } = await submitWrongCurrentPassword();
    expect(current.getAttribute("aria-invalid")).toBe("true");
    expect(current.closest("[data-slot=field]")?.textContent).toContain("临时密码不正确");
    expect(el.querySelector("[role=alert]:not([data-slot=field-error])")).toBeNull();
    unmount();
  });

  it("改动临时密码后，字段上的服务端错误消失，输入框可以继续输入", async () => {
    const { current, unmount } = await submitWrongCurrentPassword();
    act(() => type(current, "fixed-temp-password"));
    expect(current.value).toBe("fixed-temp-password");
    expect(current.getAttribute("aria-invalid")).toBeNull();
    expect(current.closest("[data-slot=field]")?.textContent).not.toContain("不正确");
    unmount();
  });
});
