import { Link } from "@tanstack/react-router";

// 员工和角色的关系与使用方法，员工页、角色页、员工详情的 ⓘ 和首页共用，说法只写一处。
// 只说明要做哪两件事，不规定先后：权限每次请求都实时检查，先分配后勾选、先勾选后分配都立即生效。
export function RoleFlowGuide() {
  return (
    <>
      <p>超级管理员拥有全部功能，不需要角色。普通员工能用哪些功能，由他的角色决定；没有角色时，登录后只能看到首页。</p>
      <p>让普通员工用上某些功能，需要做两件事（顺序不限，做完立即生效）：</p>
      <ul className="list-disc space-y-1 pl-5">
        <li>
          在
          <Link to="/roles" className="mx-1 underline underline-offset-4">
            角色
          </Link>
          页给角色勾选权限（点这一行的“权限”）；
        </li>
        <li>在员工详情里给员工分配角色。一个员工可以有多个角色，拥有这些角色的全部功能。</li>
      </ul>
    </>
  );
}
