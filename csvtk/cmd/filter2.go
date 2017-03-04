// Copyright © 2016 Wei Shen <shenwei356@gmail.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/shenwei356/xopen"
	"github.com/spf13/cobra"
)

// filter2Cmd represents the filter command
var filter2Cmd = &cobra.Command{
	Use:   "filter2",
	Short: "filter rows by awk-like artithmetic/string expressions",
	Long: `filter rows by awk-like artithmetic/string expressions

`,
	Run: func(cmd *cobra.Command, args []string) {
		config := getConfigs(cmd)
		files := getFileList(args)
		if len(files) > 1 {
			checkError(fmt.Errorf("no more than one file should be given"))
		}
		runtime.GOMAXPROCS(config.NumCPUs)

		filterStr := getFlagString(cmd, "filter")
		fuzzyFields := false

		if filterStr == "" {
			checkError(fmt.Errorf("flag -f (--filter) needed"))
		}

		if !reFilter2.MatchString(filterStr) {
			checkError(fmt.Errorf("invalid filter: %s", filterStr))
		}

		fs := make([]string, 0)
		for _, f := range reFilter2.FindAllStringSubmatch(filterStr, -1) {
			fs = append(fs, f[1])
		}

		fieldStr := strings.Join(fs, ",")

		filterStr = reFilter2VarField.ReplaceAllString(filterStr, "shenwei$1")
		filterStr = reFilter2VarSymbol.ReplaceAllString(filterStr, "")
		expression, err := govaluate.NewEvaluableExpression(filterStr)
		checkError(err)

		usingColname := true

		fields, colnames, negativeFields, needParseHeaderRow := parseFields(cmd, fieldStr, config.NoHeaderRow)
		var fieldsMap map[int]struct{}
		if len(fields) > 0 {
			usingColname = false
			fields2 := make([]int, len(fields))
			fieldsMap = make(map[int]struct{}, len(fields))
			for i, f := range fields {
				if negativeFields {
					fieldsMap[f*-1] = struct{}{}
					fields2[i] = f * -1
				} else {
					fieldsMap[f] = struct{}{}
					fields2[i] = f
				}
			}
			fields = fields2
		}

		outfh, err := xopen.Wopen(config.OutFile)
		checkError(err)
		defer outfh.Close()

		writer := csv.NewWriter(outfh)
		if config.OutTabs || config.Tabs {
			writer.Comma = '\t'
		} else {
			writer.Comma = config.OutDelimiter
		}

		for _, file := range files {
			csvReader, err := newCSVReaderByConfig(config, file)
			checkError(err)
			csvReader.Run()

			parseHeaderRow := needParseHeaderRow // parsing header row
			var colnames2fileds map[string]int   // column name -> field
			var colnamesMap map[string]*regexp.Regexp

			parameters := make(map[string]interface{}, len(colnamesMap))

			checkFields := true
			var flag bool
			var col string
			var fieldTmp int
			var value string
			var valueFloat float64
			var result interface{}

			for chunk := range csvReader.Ch {
				checkError(chunk.Err)

				for _, record := range chunk.Data {
					if parseHeaderRow { // parsing header row
						colnames2fileds = make(map[string]int, len(record))
						for i, col := range record {
							colnames2fileds[col] = i + 1
						}
						colnamesMap = make(map[string]*regexp.Regexp, len(colnames))
						for _, col := range colnames {
							if _, ok := colnames2fileds[col]; !ok {
								checkError(fmt.Errorf(`column "%s" not existed in file: %s`, col, file))
							}
							if negativeFields {
								colnamesMap[col[1:]] = fuzzyField2Regexp(col[1:])
							} else {
								colnamesMap[col] = fuzzyField2Regexp(col)
							}
						}

						if len(fields) == 0 { // user gives the colnames
							fields = []int{}
							for _, col := range record {
								var ok bool
								if fuzzyFields {
									for _, re := range colnamesMap {
										if re.MatchString(col) {
											ok = true
											break
										}
									}
								} else {
									_, ok = colnamesMap[col]
								}
								if ok {
									fields = append(fields, colnames2fileds[col])
								}
							}
						}

						fieldsMap = make(map[int]struct{}, len(fields))
						for _, f := range fields {
							fieldsMap[f] = struct{}{}
						}

						checkError(writer.Write(record))
						parseHeaderRow = false
						continue
					}
					if checkFields {
						for field := range fieldsMap {
							if field > len(record) {
								checkError(fmt.Errorf(`field (%d) out of range (%d) in file: %s`, field, len(record), file))
							}
						}
						fields2 := []int{}
						for f := range record {
							_, ok := fieldsMap[f+1]
							if negativeFields {
								if !ok {
									fields2 = append(fields2, f+1)
								}
							} else {
								if ok {
									fields2 = append(fields2, f+1)
								}
							}
						}
						fields = fields2
						if len(fields) == 0 {
							checkError(fmt.Errorf("no fields matched in file: %s", file))
						}
						fieldsMap = make(map[int]struct{}, len(fields))
						for _, f := range fields {
							fieldsMap[f] = struct{}{}
						}

						checkFields = false
					}

					flag = false

					if !usingColname {
						for _, fieldTmp = range fields {
							value = record[fieldTmp-1]
							if reDigitals.MatchString(value) {
								valueFloat, _ = strconv.ParseFloat(removeComma(value), 64)
								parameters[fmt.Sprintf("shenwei%d", fieldTmp)] = valueFloat
							} else {
								parameters[fmt.Sprintf("shenwei%d", fieldTmp)] = value
							}
						}
					} else {
						for col = range colnamesMap {
							value = record[colnames2fileds[col]-1]
							if reDigitals.MatchString(value) {
								valueFloat, _ = strconv.ParseFloat(removeComma(value), 64)
								parameters[col] = valueFloat
							} else {
								parameters[col] = value
							}
						}
					}

					result, err = expression.Evaluate(parameters)
					if err != nil {
						flag = false
						continue
					}
					switch result.(type) {
					case bool:
						if result.(bool) == true {
							flag = true
						}
					default:
						checkError(fmt.Errorf("filter is not boolean expression: %s", filterStr))
					}

					if !flag {
						continue
					}

					checkError(writer.Write(record))
				}
			}
		}
		writer.Flush()
		checkError(writer.Error())
	},
}

func init() {
	RootCmd.AddCommand(filter2Cmd)
	filter2Cmd.Flags().StringP("filter", "f", "", `awk-like filter condition. e.g. -f '$age>12' or -f '$1 > $3' or -f '$name=="abc"'`)
}

var reFilter2 = regexp.MustCompile(`\$([^ +-/*&\|^%><!~=()]+)`)
var reFilter2VarField = regexp.MustCompile(`\$(\d+)`)
var reFilter2VarSymbol = regexp.MustCompile(`\$`)
