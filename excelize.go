// Copyright 2016 - 2019 The excelize Authors. All rights reserved. Use of
// this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

// Package excelize providing a set of functions that allow you to write to
// and read from XLSX files. Support reads and writes XLSX file generated by
// Microsoft Excel™ 2007 and later. Support save file without losing original
// charts of XLSX. This library needs Go version 1.8 or later.
//
// See https://xuri.me/excelize for more information about this package.
package excelize

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

// File define a populated XLSX file struct.
type File struct {
	checked          map[string]bool
	sheetMap         map[string]string
	CalcChain        *xlsxCalcChain
	Comments         map[string]*xlsxComments
	ContentTypes     *xlsxTypes
	DrawingRels      map[string]*xlsxWorkbookRels
	Drawings         map[string]*xlsxWsDr
	Path             string
	SharedStrings    *xlsxSST
	Sheet            map[string]*xlsxWorksheet
	SheetCount       int
	Styles           *xlsxStyleSheet
	Theme            *xlsxTheme
	DecodeVMLDrawing map[string]*decodeVmlDrawing
	VMLDrawing       map[string]*vmlDrawing
	WorkBook         *xlsxWorkbook
	WorkBookRels     *xlsxWorkbookRels
	WorkSheetRels    map[string]*xlsxWorkbookRels
	XLSX             map[string][]byte
}

// OpenFile take the name of an XLSX file and returns a populated XLSX file
// struct for it.
func OpenFile(filename string) (*File, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	f, err := OpenReader(file)
	if err != nil {
		return nil, err
	}
	f.Path = filename
	return f, nil
}

// OpenReader take an io.Reader and return a populated XLSX file.
func OpenReader(r io.Reader) (*File, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}

	file, sheetCount, err := ReadZipReader(zr)
	if err != nil {
		return nil, err
	}
	f := &File{
		checked:          make(map[string]bool),
		Comments:         make(map[string]*xlsxComments),
		DrawingRels:      make(map[string]*xlsxWorkbookRels),
		Drawings:         make(map[string]*xlsxWsDr),
		Sheet:            make(map[string]*xlsxWorksheet),
		SheetCount:       sheetCount,
		DecodeVMLDrawing: make(map[string]*decodeVmlDrawing),
		VMLDrawing:       make(map[string]*vmlDrawing),
		WorkSheetRels:    make(map[string]*xlsxWorkbookRels),
		XLSX:             file,
	}
	f.CalcChain = f.calcChainReader()
	f.sheetMap = f.getSheetMap()
	f.Styles = f.stylesReader()
	f.Theme = f.themeReader()
	return f, nil
}

// setDefaultTimeStyle provides a function to set default numbers format for
// time.Time type cell value by given worksheet name, cell coordinates and
// number format code.
func (f *File) setDefaultTimeStyle(sheet, axis string, format int) error {
	s, err := f.GetCellStyle(sheet, axis)
	if err != nil {
		return err
	}
	if s == 0 {
		style, _ := f.NewStyle(`{"number_format": ` + strconv.Itoa(format) + `}`)
		f.SetCellStyle(sheet, axis, axis, style)
	}
	return err
}

// workSheetReader provides a function to get the pointer to the structure
// after deserialization by given worksheet name.
func (f *File) workSheetReader(sheet string) (*xlsxWorksheet, error) {
	name, ok := f.sheetMap[trimSheetName(sheet)]
	if !ok {
		return nil, fmt.Errorf("sheet %s is not exist", sheet)
	}
	if f.Sheet[name] == nil {
		var xlsx xlsxWorksheet
		_ = xml.Unmarshal(namespaceStrictToTransitional(f.readXML(name)), &xlsx)
		if f.checked == nil {
			f.checked = make(map[string]bool)
		}
		ok := f.checked[name]
		if !ok {
			checkSheet(&xlsx)
			checkRow(&xlsx)
			f.checked[name] = true
		}
		f.Sheet[name] = &xlsx
	}
	return f.Sheet[name], nil
}

// checkSheet provides a function to fill each row element and make that is
// continuous in a worksheet of XML.
func checkSheet(xlsx *xlsxWorksheet) {
	row := len(xlsx.SheetData.Row)
	if row >= 1 {
		lastRow := xlsx.SheetData.Row[row-1].R
		if lastRow >= row {
			row = lastRow
		}
	}
	sheetData := xlsxSheetData{}
	existsRows := map[int]int{}
	for k := range xlsx.SheetData.Row {
		if xlsx.SheetData.Row[k].R == 0 {
			xlsx.SheetData.Row[k].R = k + 1
			for j := range xlsx.SheetData.Row[k].C {
				if xlsx.SheetData.Row[k].C[j].R == "" {
					colName, _ := ColumnNumberToName(j + 1)
					xlsx.SheetData.Row[k].C[j].R = fmt.Sprintf("%s%d", colName, k+1)
				} else {
					break
				}
			}
		}
		existsRows[xlsx.SheetData.Row[k].R] = k
	}
	for i := 0; i < row; i++ {
		_, ok := existsRows[i+1]
		if ok {
			sheetData.Row = append(sheetData.Row, xlsx.SheetData.Row[existsRows[i+1]])
		} else {
			sheetData.Row = append(sheetData.Row, xlsxRow{
				R: i + 1,
			})
		}
	}
	xlsx.SheetData = sheetData
}

// replaceWorkSheetsRelationshipsNameSpaceBytes provides a function to replace
// xl/worksheets/sheet%d.xml XML tags to self-closing for compatible Microsoft
// Office Excel 2007.
func replaceWorkSheetsRelationshipsNameSpaceBytes(workbookMarshal []byte) []byte {
	var oldXmlns = []byte(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	var newXmlns = []byte(`<worksheet xr:uid="{00000000-0001-0000-0000-000000000000}" xmlns:xr3="http://schemas.microsoft.com/office/spreadsheetml/2016/revision3" xmlns:xr2="http://schemas.microsoft.com/office/spreadsheetml/2015/revision2" xmlns:xr="http://schemas.microsoft.com/office/spreadsheetml/2014/revision" xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main" xmlns:x14ac="http://schemas.microsoft.com/office/spreadsheetml/2009/9/ac" mc:Ignorable="x14ac xr xr2 xr3" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:mx="http://schemas.microsoft.com/office/mac/excel/2008/main" xmlns:mv="urn:schemas-microsoft-com:mac:vml" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	workbookMarshal = bytes.Replace(workbookMarshal, oldXmlns, newXmlns, -1)
	return workbookMarshal
}

// UpdateLinkedValue fix linked values within a spreadsheet are not updating in
// Office Excel 2007 and 2010. This function will be remove value tag when met a
// cell have a linked value. Reference
// https://social.technet.microsoft.com/Forums/office/en-US/e16bae1f-6a2c-4325-8013-e989a3479066/excel-2010-linked-cells-not-updating
//
// Notice: after open XLSX file Excel will be update linked value and generate
// new value and will prompt save file or not.
//
// For example:
//
//    <row r="19" spans="2:2">
//        <c r="B19">
//            <f>SUM(Sheet2!D2,Sheet2!D11)</f>
//            <v>100</v>
//         </c>
//    </row>
//
// to
//
//    <row r="19" spans="2:2">
//        <c r="B19">
//            <f>SUM(Sheet2!D2,Sheet2!D11)</f>
//        </c>
//    </row>
//
func (f *File) UpdateLinkedValue() error {
	for _, name := range f.GetSheetMap() {
		xlsx, err := f.workSheetReader(name)
		if err != nil {
			return err
		}
		for indexR := range xlsx.SheetData.Row {
			for indexC, col := range xlsx.SheetData.Row[indexR].C {
				if col.F != nil && col.V != "" {
					xlsx.SheetData.Row[indexR].C[indexC].V = ""
					xlsx.SheetData.Row[indexR].C[indexC].T = ""
				}
			}
		}
	}
	return nil
}

// GetMergeCells provides a function to get all merged cells from a worksheet currently.
func (f *File) GetMergeCells(sheet string) ([]MergeCell, error) {
	var mergeCells []MergeCell
	xlsx, err := f.workSheetReader(sheet)
	if err != nil {
		return mergeCells, err
	}
	if xlsx.MergeCells != nil {
		mergeCells = make([]MergeCell, 0, len(xlsx.MergeCells.Cells))

		for i := range xlsx.MergeCells.Cells {
			ref := xlsx.MergeCells.Cells[i].Ref
			axis := strings.Split(ref, ":")[0]
			val, _ := f.GetCellValue(sheet, axis)
			mergeCells = append(mergeCells, []string{ref, val})
		}
	}

	return mergeCells, err
}

// MergeCell define a merged cell data.
// It consists of the following structure.
// example: []string{"D4:E10", "cell value"}
type MergeCell []string

// GetCellValue returns merged cell value.
func (m *MergeCell) GetCellValue() string {
	return (*m)[1]
}

// GetStartAxis returns the merge start axis.
// example: "C2"
func (m *MergeCell) GetStartAxis() string {
	axis := strings.Split((*m)[0], ":")
	return axis[0]
}

// GetEndAxis returns the merge end axis.
// example: "D4"
func (m *MergeCell) GetEndAxis() string {
	axis := strings.Split((*m)[0], ":")
	return axis[1]
}
