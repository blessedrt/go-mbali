Pod::Spec.new do |spec|
  spec.name         = 'gombl'
  spec.version      = '{{.Version}}'
  spec.license      = { :type => 'GNU Lesser General Public License, Version 3.0' }
  spec.homepage     = 'https://github.com/mbali/go-mbali'
  spec.authors      = { {{range .Contributors}}
		'{{.Name}}' => '{{.Email}}',{{end}}
	}
  spec.summary      = 'iOS mbali Client'
  spec.source       = { :git => 'https://github.com/mbali/go-mbali.git', :commit => '{{.Commit}}' }

	spec.platform = :ios
  spec.ios.deployment_target  = '9.0'
	spec.ios.vendored_frameworks = 'Frameworks/gombl.framework'

	spec.prepare_command = <<-CMD
    curl https://gomblstore.blob.core.windows.net/builds/{{.Archive}}.tar.gz | tar -xvz
    mkdir Frameworks
    mv {{.Archive}}/gombl.framework Frameworks
    rm -rf {{.Archive}}
  CMD
end
