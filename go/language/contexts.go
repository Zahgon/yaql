package language

import (
	"regexp"
	"strings"
)

// Context holds data and registered functions, chained to a parent.
type Context interface {
	Parent() Context
	Convention() Convention
	Get(name string) (Value, bool)
	Set(name string, value Value)
	Delete(name string)
	RegisterFunction(fd *FunctionDefinition)
	RegisterExclusive(fd *FunctionDefinition)
	DeleteFunction(fd *FunctionDefinition)
	// GetFunctions returns this layer's overloads of name accepted by
	// predicate and whether the name is exclusive to this layer.
	GetFunctions(name string, predicate func(*FunctionDefinition) bool, useConvention bool) ([]*FunctionDefinition, bool)
	HasFunction(fd *FunctionDefinition) bool
	CreateChildContext() Context
	Keys() []string
}

// Convention converts Python-style names to the names exposed in yaql.
type Convention interface {
	ConvertFunctionName(name string) string
	ConvertParameterName(name string) string
}

// CamelCaseConvention turns snake_case into camelCase.
type CamelCaseConvention struct{}

var camelRegex = regexp.MustCompile(`_(\w)`)

func toCamelCase(name string) string {
	if name == "" {
		return name
	}
	head, rest := name[:1], name[1:]
	return head + camelRegex.ReplaceAllStringFunc(rest, func(m string) string {
		return strings.ToUpper(m[1:])
	})
}

func (CamelCaseConvention) ConvertFunctionName(name string) string  { return toCamelCase(name) }
func (CamelCaseConvention) ConvertParameterName(name string) string { return toCamelCase(name) }

// PythonConvention keeps names unchanged.
type PythonConvention struct{}

func (PythonConvention) ConvertFunctionName(name string) string  { return name }
func (PythonConvention) ConvertParameterName(name string) string { return name }

// BaseContext is the standard Context implementation.
type BaseContext struct {
	parent     Context
	convention Convention
	functions  map[string][]*FunctionDefinition
	exclusive  map[string]bool
	data       map[string]Value
	dataOrder  []string
}

// NewContext creates a context; a nil convention inherits the parent's.
func NewContext(parent Context, convention Convention) *BaseContext {
	if convention == nil && parent != nil {
		convention = parent.Convention()
	}
	return &BaseContext{
		parent:     parent,
		convention: convention,
		functions:  map[string][]*FunctionDefinition{},
		exclusive:  map[string]bool{},
		data:       map[string]Value{},
	}
}

func (c *BaseContext) Parent() Context {
	if c.parent == nil {
		return nil
	}
	return c.parent
}

func (c *BaseContext) Convention() Convention { return c.convention }

func normalizeName(name string) string {
	if !strings.HasPrefix(name, "$") {
		name = "$" + name
	}
	if name == "$" {
		name = "$1"
	}
	return name
}

func (c *BaseContext) Get(name string) (Value, bool) {
	name = normalizeName(name)
	if v, ok := c.data[name]; ok {
		return v, true
	}
	for p := c.Parent(); p != nil; p = p.Parent() {
		if bc, ok := p.(*BaseContext); ok {
			if v, ok := bc.data[name]; ok {
				return v, true
			}
			continue
		}
		if v, ok := p.Get(name); ok {
			return v, true
		}
		return nil, false
	}
	return nil, false
}

func (c *BaseContext) Set(name string, value Value) {
	name = normalizeName(name)
	if _, ok := c.data[name]; !ok {
		c.dataOrder = append(c.dataOrder, name)
	}
	c.data[name] = value
}

func (c *BaseContext) Delete(name string) {
	name = normalizeName(name)
	if _, ok := c.data[name]; !ok {
		Raise("KeyError", "%s", StringRepr(name))
	}
	delete(c.data, name)
}

func (c *BaseContext) Keys() []string {
	var out []string
	for _, k := range c.dataOrder {
		if _, ok := c.data[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func (c *BaseContext) RegisterFunction(fd *FunctionDefinition) {
	if fd.IsMethod && !fd.isValidMethod() {
		Raise("InvalidMethodException", "Function '%s' cannot be called as a method", fd.Name)
	}
	for _, existing := range c.functions[fd.Name] {
		if existing == fd {
			return
		}
	}
	c.functions[fd.Name] = append(c.functions[fd.Name], fd)
}

func (c *BaseContext) RegisterExclusive(fd *FunctionDefinition) {
	c.RegisterFunction(fd)
	c.exclusive[fd.Name] = true
}

func (c *BaseContext) DeleteFunction(fd *FunctionDefinition) {
	list := c.functions[fd.Name]
	for i, existing := range list {
		if existing == fd {
			c.functions[fd.Name] = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	delete(c.exclusive, fd.Name)
}

func (c *BaseContext) GetFunctions(name string, predicate func(*FunctionDefinition) bool, useConvention bool) ([]*FunctionDefinition, bool) {
	name = strings.TrimRight(name, "_")
	if useConvention && c.convention != nil {
		name = c.convention.ConvertFunctionName(name)
	}
	var out []*FunctionDefinition
	for _, fd := range c.functions[name] {
		if predicate == nil || predicate(fd) {
			out = append(out, fd)
		}
	}
	return out, c.exclusive[name]
}

func (c *BaseContext) HasFunction(fd *FunctionDefinition) bool {
	for _, existing := range c.functions[fd.Name] {
		if existing == fd {
			return true
		}
	}
	return false
}

func (c *BaseContext) CreateChildContext() Context { return NewContext(c, nil) }

// CollectFunctions gathers overloads of name layer by layer, innermost first.
func CollectFunctions(ctx Context, name string, predicate func(*FunctionDefinition, Context) bool, useConvention bool) [][]*FunctionDefinition {
	var layers [][]*FunctionDefinition
	for p := ctx; p != nil; {
		layer := p
		var pred func(*FunctionDefinition) bool
		if predicate != nil {
			pred = func(fd *FunctionDefinition) bool { return predicate(fd, layer) }
		}
		overloads, exclusive := p.GetFunctions(name, pred, useConvention)
		if exclusive {
			p = nil
		} else {
			p = p.Parent()
		}
		if len(overloads) > 0 {
			layers = append(layers, overloads)
		}
	}
	return layers
}
