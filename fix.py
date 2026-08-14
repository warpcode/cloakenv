import re

with open("internal/engine/orchestrator.go", "r") as f:
    content = f.read()

def replace_loop(content, case_str):
    if case_str == "[]string:":
        pattern = r"\tLoopStr:\n\t\tfor i, v := range typedVal \{(.*?^\t\t\})\n"
    else:
        pattern = r"\tLoopAny:\n\t\tfor i, v := range typedVal \{(.*?^\t\t\})\n"

    replacement = f"""		numWorkers := len(typedVal)
		if numWorkers > maxConcurrency {{
			numWorkers = maxConcurrency
		}}
		var idx int32

		for i := 0; i < numWorkers; i++ {{
			select {{
			case o.concurrencySem <- struct{{}}{{}}:
				wg.Add(1)
				go func() {{
					defer wg.Done()
					defer func() {{ <-o.concurrencySem }}()

					for {{
						i := int(atomic.AddInt32(&idx, 1) - 1)
						if i >= len(typedVal) {{
							break
						}}

						select {{
						case <-ctx.Done():
							return
						default:
						}}

						v := typedVal[i]
						res, err := o.resolveAttrRecursive(ctx, v, depth)
						if err != nil {{
							mu.Lock()
							if firstErr == nil {{
								firstErr = err
								cancel()
							}}
							mu.Unlock()
							return
						}}
"""
    if case_str == "[]string:":
        replacement += """						if str, ok := res.(string); ok {
							resolvedSlice[i] = str
						} else {
							resolvedSlice[i] = fmt.Sprintf("%v", res)
						}
"""
    else:
        replacement += """						resolvedSlice[i] = res
"""
    replacement += """					}
				}()
			default:
				break
			}
		}

		for {
			i := int(atomic.AddInt32(&idx, 1) - 1)
			if i >= len(typedVal) {
				break
			}
			if ctx.Err() != nil {
				break
			}
			v := typedVal[i]
			res, err := o.resolveAttrRecursive(ctx, v, depth)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				break
			}
"""
    if case_str == "[]string:":
        replacement += """			if str, ok := res.(string); ok {
				resolvedSlice[i] = str
			} else {
				resolvedSlice[i] = fmt.Sprintf("%v", res)
			}
"""
    else:
        replacement += """			resolvedSlice[i] = res
"""
    replacement += """		}
"""

    return re.sub(pattern, replacement, content, flags=re.DOTALL | re.MULTILINE)

content = replace_loop(content, "[]string:")
content = replace_loop(content, "[]any:")

if '"sync/atomic"' not in content:
    import_idx = content.find('"sync"\n')
    content = content[:import_idx] + '"sync"\n\t"sync/atomic"\n' + content[import_idx+7:]

with open("internal/engine/orchestrator.go", "w") as f:
    f.write(content)
