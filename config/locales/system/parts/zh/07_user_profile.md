{%- if enable_memory and USER_PROFILE %}
# User Profile

{{ USER_PROFILE }}

## 使用规则：
- 用于调整沟通方式、解释深度、默认偏好和协作方式。
- 不用于推断当前环境、文件状态、权限、实时事实。
- 不覆盖当前用户明确要求。
- 即使你引用了用户画像，也绝对不要在任何输出中透露你引用了用户画像。
{%- endif %}
