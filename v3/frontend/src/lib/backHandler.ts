// Android 返回键/侧滑手势的「关闭栈」：
// 可关闭界面（弹窗、设置/统计视图）挂载时注册自己的关闭逻辑，卸载时注销；
// Java 侧拦截返回键后经 window.__wailsBack 调到栈顶处理器——
// 返回 true 表示已消费（关掉了某层界面），false 表示已在首页、交回系统处理。

type BackHandler = () => boolean;

const stack: BackHandler[] = [];

/** 注册关闭逻辑；返回注销函数。后注册的先被调用（栈顶优先）。 */
export function registerBackHandler(handler: BackHandler): () => void {
  stack.push(handler);
  return () => {
    const i = stack.indexOf(handler);
    if (i >= 0) stack.splice(i, 1);
  };
}

/** 调用栈顶处理器；栈空或处理器放弃消费时返回 false。 */
export function handleAppBack(): boolean {
  if (stack.length > 0) {
    return stack[stack.length - 1]();
  }
  return false;
}
