package diagnostics

import (
	"fmt"
	"io"
	"strings"

	"github.com/kudobuilder/kudo/pkg/kudoctl/util/kudo"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v2"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	DiagDir = "diag"
	KudoDir = "diag/kudo"
)

type printMode int

const (
	ObjectWithDir      printMode = iota // print object into its own nested directory based on its name and kind
	ObjectListWithDirs                  // print each object into its own nested directory based on its name and kind
	RuntimeObject                       // print as a file based on its kind only
)

// NonFailingPrinter - print provided data into provided directory and accumulate errors instead of returning them.
// Creates a nested directory if an object type requires so.
type NonFailingPrinter struct {
	fs     afero.Fs
	errors []string
}

func (p *NonFailingPrinter) printObject(o runtime.Object, parentDir string, mode printMode) {
	if err := printRuntimeObject(p.fs, o, parentDir, mode); err != nil {
		p.errors = append(p.errors, err.Error())
	}
}

func (p *NonFailingPrinter) printError(err error, parentDir, name string) {
	b := []byte(err.Error())
	if err := printBytes(p.fs, b, parentDir, fmt.Sprintf("%s.err", name)); err != nil {
		p.errors = append(p.errors, err.Error())
	}
}

func (p *NonFailingPrinter) printLog(log io.ReadCloser, parentDir, name string) {
	if err := printLog(p.fs, log, parentDir, name); err != nil {
		p.errors = append(p.errors, err.Error())
	}
}

func (p *NonFailingPrinter) printYaml(v interface{}, parentDir, name string) {
	if err := printYaml(p.fs, v, parentDir, name); err != nil {
		p.errors = append(p.errors, err.Error())
	}
}

func printRuntimeObject(fs afero.Fs, obj runtime.Object, parentDir string, mode printMode) error {
	switch mode {
	case ObjectWithDir:
		return printSingleObject(fs, obj, parentDir)
	case ObjectListWithDirs:
		return meta.EachListItem(obj, func(ro runtime.Object) error {
			return printSingleObject(fs, ro, parentDir)
		})
	case RuntimeObject:
		fallthrough
	default:
		return printSingleRuntimeObject(fs, obj, parentDir)
	}
}

// printSingleObject - print a runtime.Object assuming it exposes metadata by implementing metav1.Object
// or panic otherwise. Object is put into a nested directory.
func printSingleObject(fs afero.Fs, obj runtime.Object, parentDir string) error {
	if !isKudoCR(obj) {
		err := kudo.SetGVKFromScheme(obj, scheme.Scheme)
		if err != nil {
			return err
		}
	}
	o, _ := obj.(Object)
	relToParentDir := fmt.Sprintf("%s_%s", strings.ToLower(o.GetObjectKind().GroupVersionKind().Kind), o.GetName())
	dir := fmt.Sprintf("%s/%s", parentDir, relToParentDir)
	err := fs.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}
	fileWithPath := fmt.Sprintf("%s/%s.yaml", dir, o.GetName())
	file, err := fs.Create(fileWithPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", fileWithPath, err)
	}
	printer := printers.YAMLPrinter{}
	return printer.PrintObj(o, file)
}

func printSingleRuntimeObject(fs afero.Fs, obj runtime.Object, dir string) error {
	err := kudo.SetGVKFromScheme(obj, scheme.Scheme)
	if err != nil {
		return err
	}
	err = fs.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}
	fileWithPath := fmt.Sprintf("%s/%s.yaml", dir, strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind))
	file, err := fs.Create(fileWithPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", fileWithPath, err)
	}
	printer := printers.YAMLPrinter{}
	return printer.PrintObj(obj, file)
}

func printLog(fs afero.Fs, log io.ReadCloser, parentDir, podName string) error {
	dir := fmt.Sprintf("%s/pod_%s", parentDir, podName)
	err := fs.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}
	fileNameWithPath := fmt.Sprintf("%s/%s.log.gz", dir, podName)
	file, err := fs.Create(fileNameWithPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %v", fileNameWithPath, err)
	}
	z := newGzipWriter(file, 2048)
	err = z.Write(log)
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %v", fileNameWithPath, err)
	}
	_ = log.Close()
	return nil
}

func printYaml(fs afero.Fs, v interface{}, dir, name string) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal object to %s/%s.yaml: %v", dir, name, err)
	}
	err = fs.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}
	name = fmt.Sprintf("%s.yaml", name)
	return printBytes(fs, b, dir, name)
}

func printBytes(fs afero.Fs, b []byte, dir, name string) error {
	err := fs.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}
	fileNameWithPath := fmt.Sprintf("%s/%s", dir, name)
	file, err := fs.Create(fileNameWithPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %v", fileNameWithPath, err)
	}
	_, err = file.Write(b)
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %v", fileNameWithPath, err)
	}
	return nil
}
