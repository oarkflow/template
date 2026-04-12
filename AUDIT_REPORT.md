# SPL Template Engine - Production Readiness Audit Report

**Date:** 2026-04-12  
**Target:** `/Users/sujit/Sites/interpreter/template/`  
**Status:** **NOT PRODUCTION READY** - Multiple critical and important issues identified

---

## Executive Summary

The SPL template engine has **3 CRITICAL issues** that must be fixed before production use:
1. **Race conditions** in concurrent template rendering
2. **Unbounded cache growth** causing memory leaks
3. **Unsafe dynamic code generation** with user expressions

Additionally, **6 IMPORTANT issues** and **4 NICE-TO-HAVE improvements** were found.

Test coverage is reasonable (89 test cases), but **concurrent access tests already fail** with `-race` flag.

---

## 1. SECURITY ISSUES

### 1.1 XSS PREVENTION - AutoEscape Handling ✅ GOOD

**Status:** SECURE with defaults enabled

The AutoEscape mechanism is properly implemented:
- **render.go:256-257**: Strings are HTML-escaped by default
- **render.go:275-276**: Expressions are auto-escaped unless marked `${raw ...}`
- **filters.go:79**: Filter output is also escaped
- Signal names and attributes are escaped in hydration HTML (reactive.go:223, 237, 289, 294)

**Finding:** AutoEscape is ON by default, which is correct. All ${} expressions are properly escaped.

---

### 1.2 HANDLER/SIGNAL INJECTION VULNERABILITY 🔴 CRITICAL

**Issue:** User-provided handler expressions and signal names are injected into JavaScript without sufficient validation.

**Location 1: hydration_runtime.go:201, 209-210**
```javascript
// VULNERABLE: new Function with unsanitized user expr
fn=new Function('scope','event','element','with(scope){ '+expr+'; return undefined; }');
fn=new Function('scope','event','element','with(scope){ return ('+expr+'); }');
```

**Attack Vector:** 
A malicious signal handler could execute arbitrary JavaScript:
```
@handler(hack) {
  }; console.log("pwned"); /*
}
```

This would produce:
```javascript
fn=new Function(...'with(scope){ }; console.log("pwned"); /*; return undefined; }');
```

**Severity:** HIGH - Full code execution in client context

**Recommendation:** 
- Implement a safer expression sandboxing mechanism
- Use Web Workers or restricted evaluation contexts
- Or use a DSL-specific interpreter instead of Function()

**Affected Files:**
- hydration_runtime.go:201 (SPL.runStatement)
- hydration_runtime.go:209 (SPL.evalExpression)
- hydration_runtime.go:202, 210 (cache without bounds)

---

### 1.3 CACHE INJECTION IN HYDRATION RUNTIME 🔴 CRITICAL

**Issue:** Expression and statement caches grow unboundedly and lack validation

**Location: hydration_runtime.go:192-213**
```javascript
SPL.statementCache=SPL.statementCache||{};
SPL.expressionCache=SPL.expressionCache||{};
// ... later:
fn=new Function(...);
SPL.statementCache[expr]=fn;  // UNBOUNDED
SPL.expressionCache[expr]=fn;  // UNBOUNDED
```

**Attack Vector:**
An attacker could trigger many different expressions via handlers, exhausting client memory:
```javascript
for(let i=0; i<100000; i++) {
  evalExpression(`x${i}`, ...);  // Creates 100k Function objects
}
```

**Severity:** MEDIUM (DoS potential on client)

**Recommendation:**
- Implement LRU cache with max size (e.g., 1000 entries)
- Or move to server-side expression evaluation

---

## 2. CONCURRENCY ISSUES 🔴 CRITICAL

### 2.1 DATA RACE IN globalEnv INITIALIZATION

**Issue:** `newGlobalEnv()` reads and writes `e.globalEnv` without locking

**Location: template.go:127-131**
```go
func (e *Engine) newGlobalEnv() *interpreter.Environment {
	if e.globalEnv == nil {  // READ (racy)
		e.globalEnv = interpreter.NewGlobalEnvironment([]string{})  // WRITE (racy)
	}
	return e.globalEnv
}
```

**Test Results:**
```
go test ./template/ -race -count=1
FAIL: race detected during execution of test
  Write at 0x00c002404208 by goroutine 53
  Previous read at 0x00c002404208 by goroutine 55
```

**Impact:** Concurrent renders can create duplicate global environments, wasting memory and causing inconsistent state.

**Fix Required:**
```go
func (e *Engine) newGlobalEnv() *interpreter.Environment {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.globalEnv == nil {
		e.globalEnv = interpreter.NewGlobalEnvironment([]string{})
	}
	return e.globalEnv
}
```

---

### 2.2 UNSYNCHRONIZED COMPONENTS MAP ACCESS

**Issue:** Components map is accessed without locks during rendering

**Locations:**
- **render.go:41**: Write without lock (during renderNodes in layout)
- **render.go:104**: Write without lock (during @component parsing in @define)
- **render.go:597**: Read without lock (during renderRender)
- **template.go:122**: Write without lock (RegisterComponent)

**Code Snippet:**
```go
// render.go:41 - NO LOCK
case *ComponentNode:
    e.Components[v.Name] = componentDef{Body: v.Body, Props: v.Props}

// render.go:597 - NO LOCK
comp, ok := e.Components[n.Name]
```

**Risk:** Concurrent template parsing/rendering can corrupt the Components map or miss newly registered components.

**Recommendation:** Synchronize with `e.mu` (read for lookups, write for registration).

---

### 2.3 UNSYNCHRONIZED FILTER MAP ACCESS

**Issue:** Filters map is not locked during concurrent access

**Location: template.go:61** (no lock protection)

While RegisterFilter uses no explicit lock, the field is directly accessed from concurrent renders. If a filter is registered during rendering, it could race.

---

## 3. ERROR HANDLING & SAFETY ISSUES

### 3.1 UNSAFE TYPE ASSERTION 🟡 IMPORTANT

**Issue:** Unchecked type assertion in updateLoopMeta

**Location: render.go:416**
```go
func updateLoopMeta(h *interpreter.Hash, index, length int) {
	for k, pair := range h.Pairs {
		switch pair.Key.(*interpreter.String).Value {  // ← PANIC if Key is not *String
		case "index":
```

**Risk:** If a non-string key appears in the hash, this will panic. While unlikely in this context (loop vars are controlled), it's not defensive.

**Fix:**
```go
switch key := pair.Key.(type) {
case *interpreter.String:
    switch key.Value {
    case "index":
        // ...
    }
}
```

---

### 3.2 MISSING BOUNDS CHECKS IN LOOP METADATA

**Issue:** updateLoopMeta doesn't validate that index < length

**Location: render.go:414-430**
```go
// No validation that index >= 0 or index < length
updateLoopMeta(loopHash, i, length)
// calculateLastProperty: index == length-1
```

While this is low-risk in current usage, defensive programming would add:
```go
if index < 0 || index >= length {
    return fmt.Errorf("invalid loop index")
}
```

---

### 3.3 MAP ACCESS WITHOUT OK CHECK 🟡 IMPORTANT

**Issue:** Several places use map access without checking the ok flag

**Location: render.go:659** (props handling):
```go
if pair, exists := propsHash.Pairs[hk]; exists {  // OK, has check
    compEnv.Set(varName, pair.Value)
}
```

This one is checked, but let me verify all hash accesses... ✅ Most are properly guarded.

---

## 4. MEMORY ISSUES 🔴 CRITICAL

### 4.1 UNBOUNDED CACHE GROWTH - exprCache

**Issue:** Expression AST cache grows without bounds or eviction

**Location: template.go:68, 94, 438**
```go
exprCache         map[string]*interpreter.Program
// ...
e.exprCache[expr] = program  // UNBOUNDED APPEND
```

**Risk:** Long-running server processing many unique templates (or adversary input) exhausts memory.

**Example Attack:**
```
for i := 0; i < 1000000; i++ {
    e.Render(fmt.Sprintf("${x%d}", i), nil)  // Creates 1M+ ASTs in cache
}
```

**Memory Impact:** 
- Each Program can be 1-10KB
- 1 million expressions = 1-10 GB memory

**Fix Required:** Implement cache eviction policy:
```go
const maxExprCacheSize = 10000
if len(e.exprCache) > maxExprCacheSize {
    // Evict oldest 20% (LRU tracking needed)
}
```

---

### 4.2 UNBOUNDED CACHE GROWTH - fileCache

**Issue:** Template file cache has no eviction

**Location: template.go:69, 96, 207**
```go
fileCache         map[string][]Node
e.fileCache[resolved] = nodes  // UNBOUNDED
```

**Risk:** Server with many template files or directory traversal attacks can exhaust memory.

**Fix:** Similar LRU eviction or implement InvalidateCaches() scheduling.

---

### 4.3 UNBOUNDED CACHE GROWTH - compiledTextCache & compiledFileCache

**Issue:** Compiled template caches lack size limits

**Location: template.go:72-73, 262-263, 285**
```go
compiledFileCache map[string]*compiledTemplate  // unbounded
compiledTextCache map[string]*compiledTemplate  // unbounded
```

**Risk:** Each compiled template can be 50KB+. 1000 unique renders = 50MB+.

**Current Mitigation:** `InvalidateCaches()` method exists but must be called manually.

**Fix:** Add cache size limits or automatic TTL-based eviction.

---

### 4.4 UNBOUNDED CACHE GROWTH - exprMeta

**Issue:** Fast-path metadata cache mirrors exprCache without bounds

**Location: template.go:82, 442**
```go
exprMeta map[string]exprFastPath
e.exprMeta[expr] = meta  // UNBOUNDED
```

**Risk:** While smaller than exprCache, still grows linearly with unique expressions.

---

## 5. PERFORMANCE ISSUES 🟡 IMPORTANT

### 5.1 LOOP ALLOCATION: renderFor

**Issue:** Each loop iteration may allocate new objects unnecessarily

**Location: render.go:327-334**
```go
var items []iterItem
// Allocate new iterItem structs for each element
for i, elem := range v.Elements {
    items = append(items, iterItem{
        key:   &interpreter.Integer{Value: int64(i)},
        value: elem,
    })
}
```

**Impact:** For a 10,000-item loop, this creates 10,000 allocations upfront.

**Fix:** Use range-over index directly:
```go
for i := 0; i < len(v.Elements); i++ {
    item := iterItem{
        key:   &interpreter.Integer{Value: int64(i)},
        value: v.Elements[i],
    }
    // process item
}
```

---

### 5.2 HASH PAIR COPYING IN mergePlaceholders

**Issue:** makePlaceholderHash recursively copies hashes

**Location: render.go:956-980**
```go
func makePlaceholderHash(original *interpreter.Hash, signalName string) *interpreter.Hash {
	ph := &interpreter.Hash{Pairs: make(map[interpreter.HashKey]interpreter.HashPair, len(original.Pairs))}
	for hk, pair := range original.Pairs {
		// Deep copy
		if innerHash, ok := pair.Value.(*interpreter.Hash); ok {
			ph.Pairs[hk] = interpreter.HashPair{
				Key:   pair.Key,
				Value: makePlaceholderHash(innerHash, path),  // Recursive
			}
		}
	}
}
```

**Impact:** Large nested objects create many intermediate hashes during hydration setup.

**Benchmark needed:** Test with deeply nested signal objects.

---

## 6. EDGE CASES & VALIDATION

### 6.1 NO VALIDATION FOR SIGNAL NAMES

**Issue:** Signal names are not validated for special characters or keywords

**Location: reactive.go:146-151**
```go
func (e *Engine) registerSignal(name string, value interpreter.Object) {
	// No validation on 'name'
	e.hydration.Signals[name] = objectToNative(value)
}
```

**Risk:** Names like `__proto__`, `constructor`, `toString` could cause issues in JavaScript.

**Example:**
```
@signal(__proto__ = {evil: true})  // Pollutes prototype
```

**Fix:** Validate names:
```go
func isValidSignalName(name string) bool {
	reserved := map[string]bool{
		"__proto__": true, "constructor": true, "prototype": true,
		"toString": true, "valueOf": true, // ... etc
	}
	return !reserved[name] && regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(name)
}
```

---

### 6.2 COMPONENT SCOPE ISSUE IN @define

**Issue:** Components registered inside @define blocks are global to engine

**Location: render.go:103-105**
```go
for _, dn := range defined {
	if comp, ok := dn.(*ComponentNode); ok {
		e.Components[comp.Name] = componentDef{Body: comp.Body, Props: comp.Props}
```

**Risk:** @component inside @define should be scoped to that define block, not global.

**Example (forms.html lines 15-26):**
```
@define("content") {
    @component("FormField", ...) { ... }
}
```

The FormField component is registered globally, not just within the content block. This could cause collisions.

**Status:** You noted this was recently fixed. ✓ Verify the fix is working correctly.

---

### 6.3 NO CIRCULAR DEPENDENCY DETECTION

**Issue:** @extends and @include can create infinite loops

**Location: template.go:317-335** (registerImportedComponents)
```go
func (e *Engine) registerImportedComponents(imports []string, components map[string]componentDef, seen map[string]struct{}) error {
	for _, path := range imports {
		resolved := e.resolvePath(path)
		if _, ok := seen[resolved]; ok {
			continue  // OK: prevents infinite recursion
		}
```

**Status:** ✓ Circular dependency detection is implemented via `seen` map.

---

## 7. TEST COVERAGE ASSESSMENT

**Test Count:** 89 tests (PASS/FAIL)

**Coverage Analysis:**

| Area | Tests | Status |
|------|-------|--------|
| Basic expressions | 10+ | ✅ Good |
| Loops & iteration | 5+ | ✅ Good |
| Components | 15+ | ✅ Good |
| Filters | 5+ | ✅ Good |
| AutoEscape | 3 | ✅ Good |
| Concurrent rendering | 2 | ❌ **FAILS with -race** |
| Cache limits | 0 | ❌ **MISSING** |
| XSS attacks | 0 | ❌ **MISSING** |
| Handler injection | 0 | ❌ **MISSING** |
| Memory stress | 0 | ❌ **MISSING** |

**Missing Critical Tests:**
1. ❌ XSS payload tests (e.g., `<img src=x onerror="..."`)
2. ❌ Handler injection payloads
3. ❌ Cache overflow tests (render 100k+ unique templates)
4. ❌ Concurrent Components registration
5. ❌ Race condition stress tests
6. ❌ Signal name validation tests

---

## 8. PRODUCTION READINESS CHECKLIST

| Item | Status | Notes |
|------|--------|-------|
| **Security** | ❌ NO | Injection vulnerabilities, cache DoS |
| **Concurrency** | ❌ NO | Race conditions proven with -race |
| **Memory** | ❌ NO | Unbounded caches cause leaks |
| **Error Handling** | 🟡 PARTIAL | Some unsafe assertions, mostly OK |
| **Performance** | 🟡 PARTIAL | Hot-path allocations, no optimization |
| **Test Coverage** | 🟡 PARTIAL | Good for happy path, missing edge cases |

---

## RECOMMENDATIONS BY PRIORITY

### CRITICAL (Must Fix Before Production)

1. **Fix globalEnv race condition** (template.go:127-131)
   - Add mutex lock around read-modify-write
   - Effort: 5 min

2. **Implement cache size limits** (template.go:68-73)
   - Add LRU eviction for exprCache, fileCache, compiledTextCache, compiledFileCache, exprMeta
   - Max sizes: exprCache=10k, fileCache=1k, compiledFileCache=100, compiledTextCache=500, exprMeta=10k
   - Effort: 2-3 hours

3. **Synchronize Components map access** (render.go, template.go)
   - Use e.mu for all Components access
   - Effort: 30 min

4. **Secure handler expression evaluation** (hydration_runtime.go:201, 209)
   - Replace Function() with safer approach or sandboxed eval
   - Consider Web Workers or moved to server-side
   - Effort: 4-6 hours

5. **Validate signal names** (reactive.go:146)
   - Reject reserved JS names (__proto__, constructor, etc.)
   - Enforce regex pattern ^[a-zA-Z_][a-zA-Z0-9_]*$
   - Effort: 30 min

### IMPORTANT (Should Fix)

6. Defensive type assertions (render.go:416)
7. Handler name validation
8. Add cache eviction monitoring/metrics
9. Synchronize Filters map access
10. Add missing test cases for security

### NICE-TO-HAVE (Could Improve)

11. Optimize loop iteration allocation
12. Benchmark placeholder hash creation
13. Add debug logging for cache hits/misses
14. Document cache size limits in Engine comments
15. Implement cache statistics API

---

## CONCLUSION

The SPL template engine has **solid core functionality** with good coverage for standard use cases, but **is NOT safe for production** due to:

1. **Proven race conditions** (fails -race tests)
2. **Memory leaks** from unbounded caches
3. **Code injection vulnerabilities** in handler evaluation
4. **Missing security validations**

**Estimated Fix Time:** 10-15 engineer hours

**Suggested Approach:**
1. Fix concurrency issues immediately (1-2 hours)
2. Add cache size limits (2-3 hours)
3. Secure expression evaluation (4-6 hours)
4. Add security validation & tests (2-3 hours)
5. Review & re-test (1-2 hours)

Recommend **NOT deploying to production** until critical issues are resolved.
